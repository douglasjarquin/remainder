package claude

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os/exec"
	"os/user"
	"time"
)

const claudeKeychainService = "Claude Code-credentials"

var ErrKeychainPromptRequired = errors.New("Claude macOS Keychain collection requires --allow-keychain-prompt")

var errKeychainOutputTooLarge = errors.New("Claude Keychain output is too large")

func (a Adapter) readKeychainCredentials(ctx context.Context) (credentials, error) {
	body, err := a.keychainReader(ctx)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return credentials{}, ctxErr
		}
		if errors.Is(err, errKeychainOutputTooLarge) {
			return credentials{}, errors.New("Claude Keychain item is too large")
		}
		return credentials{}, errors.New("Claude Keychain item could not be read")
	}
	if len(body) > maxAuthBytes {
		return credentials{}, errors.New("Claude Keychain item is too large")
	}
	return parseCredentials(body, a.now(), "Keychain item")
}

func keychainAccount() (string, error) {
	current, err := user.Current()
	if err != nil || current.Username == "" {
		return "", errors.New("Claude Keychain account could not be resolved")
	}
	return current.Username, nil
}

func readMacKeychain(ctx context.Context) ([]byte, error) {
	account, err := keychainAccount()
	if err != nil {
		return nil, err
	}
	return readMacKeychainWithRunner(ctx, account, func(command *exec.Cmd) error {
		return command.Run()
	})
}

func readMacKeychainWithRunner(ctx context.Context, account string, run func(*exec.Cmd) error) ([]byte, error) {
	var stdout boundedKeychainBuffer
	command := exec.CommandContext(ctx, "/usr/bin/security", "find-generic-password", "-a", account, "-w", "-s", claudeKeychainService)
	command.Stdout = &stdout
	command.Stderr = io.Discard
	command.WaitDelay = time.Second
	if err := run(command); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, err
	}
	if stdout.overflow {
		return nil, errKeychainOutputTooLarge
	}
	return stdout.buffer.Bytes(), nil
}

type boundedKeychainBuffer struct {
	buffer   bytes.Buffer
	overflow bool
}

func (w *boundedKeychainBuffer) Write(data []byte) (int, error) {
	remaining := maxAuthBytes + 1 - w.buffer.Len()
	if remaining > 0 {
		w.buffer.Write(data[:min(len(data), remaining)])
	}
	if len(data) > remaining || w.buffer.Len() > maxAuthBytes {
		w.overflow = true
	}
	return len(data), nil
}
