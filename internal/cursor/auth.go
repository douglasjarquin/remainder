package cursor

import (
	"encoding/base64"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const maxAuthBytes = 1 << 20

type authFile struct {
	AccessToken string `json:"accessToken"`
}

type tokenClaims struct {
	ExpiresAt *int64 `json:"exp"`
}

func defaultAuthPath(getenv func(string) (string, bool)) (string, error) {
	if value, set := getenv("CURSOR_CLI_CONFIG"); set {
		if value == "" {
			return "", fmt.Errorf("%w: CURSOR_CLI_CONFIG is explicitly empty", ErrInvalidAuth)
		}
		return value, nil
	}
	if value, set := getenv("XDG_CONFIG_HOME"); set {
		if value == "" {
			return "", fmt.Errorf("%w: XDG_CONFIG_HOME is explicitly empty", ErrInvalidAuth)
		}
		return filepath.Join(value, "cursor", "auth.json"), nil
	}
	if value, set := getenv("HOME"); set && value != "" {
		return filepath.Join(value, ".config", "cursor", "auth.json"), nil
	}
	return "", fmt.Errorf("%w: home directory is unavailable", ErrInvalidAuth)
}

func inspectAuthFile(path string) (os.FileInfo, error) {
	if path == "" {
		return nil, fmt.Errorf("%w: authentication file path is empty", ErrInvalidAuth)
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%w: Cursor CLI authentication file is missing", ErrInvalidAuth)
	}
	if err != nil {
		return nil, fmt.Errorf("%w: Cursor CLI authentication file cannot be inspected", ErrInvalidAuth)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: Cursor CLI authentication file is not a regular file", ErrInvalidAuth)
	}
	if info.Size() < 0 || info.Size() > maxAuthBytes {
		return nil, fmt.Errorf("%w: Cursor CLI authentication file is too large", ErrInvalidAuth)
	}
	if !authFileOwnedBy(info, os.Getuid()) {
		return nil, fmt.Errorf("%w: Cursor CLI authentication file is not owned by the current user", ErrInvalidAuth)
	}
	return info, nil
}

func readAccessToken(path string, now time.Time) (string, error) {
	before, err := inspectAuthFile(path)
	if err != nil {
		return "", err
	}
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return "", fmt.Errorf("%w: Cursor CLI authentication file cannot be opened safely", ErrInvalidAuth)
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		syscall.Close(fd)
		return "", fmt.Errorf("%w: Cursor CLI authentication file cannot be opened safely", ErrInvalidAuth)
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !authFileSameMetadata(before, opened, os.Getuid()) {
		return "", fmt.Errorf("%w: Cursor CLI authentication file changed during safe open", ErrInvalidAuth)
	}
	body, err := io.ReadAll(io.LimitReader(file, maxAuthBytes+1))
	if err != nil {
		return "", fmt.Errorf("%w: Cursor CLI authentication file cannot be read", ErrInvalidAuth)
	}
	if len(body) > maxAuthBytes {
		return "", fmt.Errorf("%w: Cursor CLI authentication file is too large", ErrInvalidAuth)
	}
	final, err := file.Stat()
	if err != nil || !authFileStable(opened, final, len(body), os.Getuid()) {
		return "", fmt.Errorf("%w: Cursor CLI authentication file changed during safe read", ErrInvalidAuth)
	}
	var raw authFile
	if err := json.Unmarshal(body, &raw); err != nil {
		return "", fmt.Errorf("%w: Cursor CLI authentication file is malformed", ErrInvalidAuth)
	}
	token := strings.TrimSpace(raw.AccessToken)
	if token == "" {
		return "", fmt.Errorf("%w: Cursor CLI authentication file lacks a nonempty accessToken", ErrInvalidAuth)
	}
	if expiredJWT(token, now) {
		return "", fmt.Errorf("%w: Cursor CLI access token is expired", ErrInvalidAuth)
	}
	return token, nil
}

func authFileOwnedBy(info os.FileInfo, uid int) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(uid)
}

func authFileSameMetadata(before, after os.FileInfo, uid int) bool {
	return after.Mode().IsRegular() &&
		authFileOwnedBy(after, uid) &&
		os.SameFile(before, after) &&
		after.Size() == before.Size() &&
		after.ModTime().Equal(before.ModTime())
}

func authFileStable(before, after os.FileInfo, bytesRead, uid int) bool {
	return authFileSameMetadata(before, after, uid) && after.Size() == int64(bytesRead)
}

func expiredJWT(token string, now time.Time) bool {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return false
	}
	var claims tokenClaims
	if err := json.Unmarshal(payload, &claims); err != nil || claims.ExpiresAt == nil {
		return false
	}
	return *claims.ExpiresAt <= now.Unix()
}
