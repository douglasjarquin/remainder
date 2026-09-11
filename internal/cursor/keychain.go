package cursor

import (
	"bytes"
	"context"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const maxKeychainBytes = 1 << 20

var ErrKeychainPromptRequired = fmt.Errorf("%w: macOS Cursor collection requires --allow-keychain-prompt", ErrInvalidAuth)

var errKeychainOutputTooLarge = errors.New("Cursor CLI Keychain output is too large")

type macConfig struct {
	AuthInfo *macAuthInfo `json:"authInfo"`
}

type macAuthInfo struct {
	Email  identityHint `json:"email"`
	UserID identityHint `json:"userId"`
	AuthID identityHint `json:"authId"`
}

type identityHint string

func (h *identityHint) UnmarshalJSON(data []byte) error {
	*h = ""
	if len(data) == 0 || data[0] != '"' {
		return nil
	}
	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		return err
	}
	*h = identityHint(text)
	return nil
}

type macConfigSnapshot struct {
	fingerprint string
}

func defaultMacConfigPath(getenv func(string) (string, bool)) (string, error) {
	if value, set := getenv("CURSOR_CLI_CONFIG"); set {
		if value == "" {
			return "", fmt.Errorf("%w: CURSOR_CLI_CONFIG is explicitly empty", ErrInvalidAuth)
		}
		return value, nil
	}
	if value, set := getenv("HOME"); set && value != "" {
		return filepath.Join(value, ".cursor", "cli-config.json"), nil
	}
	return "", fmt.Errorf("%w: home directory is unavailable", ErrInvalidAuth)
}

func readMacConfig(path string) (macConfigSnapshot, error) {
	body, info, err := readOwnedRegularFile(path, "Cursor CLI configuration file")
	if err != nil {
		return macConfigSnapshot{}, err
	}
	var config macConfig
	if err := json.Unmarshal(body, &config); err != nil {
		return macConfigSnapshot{}, fmt.Errorf("%w: Cursor CLI configuration file is malformed", ErrInvalidAuth)
	}
	if config.AuthInfo == nil {
		return macConfigSnapshot{}, fmt.Errorf("%w: Cursor CLI configuration file lacks authInfo identity", ErrInvalidAuth)
	}
	userID := strings.TrimSpace(string(config.AuthInfo.UserID))
	if userID == "" {
		userID = strings.TrimSpace(string(config.AuthInfo.AuthID))
	}
	email := strings.TrimSpace(string(config.AuthInfo.Email))
	if userID == "" && email == "" {
		return macConfigSnapshot{}, fmt.Errorf("%w: Cursor CLI configuration file lacks authInfo identity", ErrInvalidAuth)
	}
	return macConfigSnapshot{fingerprint: fileFingerprint(path, info, userID+"\x00"+email)}, nil
}

func (a Adapter) readMacAccessToken(ctx context.Context) (string, error) {
	before, err := readMacConfig(a.configFile)
	if err != nil {
		return "", err
	}
	raw, err := a.keychainReader(ctx)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", ctxErr
		}
		if errors.Is(err, errKeychainOutputTooLarge) {
			return "", fmt.Errorf("%w: Cursor CLI Keychain access token is too large", ErrInvalidAuth)
		}
		return "", fmt.Errorf("%w: Cursor CLI Keychain access token could not be read", ErrInvalidAuth)
	}
	if len(raw) > maxKeychainBytes {
		return "", fmt.Errorf("%w: Cursor CLI Keychain access token is too large", ErrInvalidAuth)
	}
	token := strings.TrimSpace(raw)
	if token == "" {
		return "", fmt.Errorf("%w: Cursor CLI Keychain access token is empty", ErrInvalidAuth)
	}
	if expiredJWT(token, a.now()) {
		return "", fmt.Errorf("%w: Cursor CLI Keychain access token is expired", ErrInvalidAuth)
	}
	after, err := readMacConfig(a.configFile)
	if err != nil || before.fingerprint != after.fingerprint {
		return "", fmt.Errorf("%w: Cursor CLI configuration file changed during Keychain access", ErrInvalidAuth)
	}
	return token, nil
}

func readMacKeychain(ctx context.Context) (string, error) {
	return readMacKeychainWithRunner(ctx, func(command *exec.Cmd) error {
		return command.Run()
	})
}

func readMacKeychainWithRunner(ctx context.Context, run func(*exec.Cmd) error) (string, error) {
	var stdout boundedBuffer
	command := exec.CommandContext(ctx, "/usr/bin/security", "find-generic-password", "-a", "cursor-user", "-w", "-s", "cursor-access-token")
	command.Stdout = &stdout
	command.Stderr = io.Discard
	command.WaitDelay = time.Second
	if err := run(command); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", ctxErr
		}
		return "", err
	}
	if stdout.overflow {
		return "", errKeychainOutputTooLarge
	}
	return stdout.String(), nil
}

type boundedBuffer struct {
	buffer   bytes.Buffer
	overflow bool
}

func (w *boundedBuffer) Write(data []byte) (int, error) {
	remaining := maxKeychainBytes + 1 - w.buffer.Len()
	if remaining > 0 {
		w.buffer.Write(data[:min(len(data), remaining)])
	}
	if len(data) > remaining || w.buffer.Len() > maxKeychainBytes {
		w.overflow = true
	}
	return len(data), nil
}

func (w *boundedBuffer) String() string {
	return w.buffer.String()
}
