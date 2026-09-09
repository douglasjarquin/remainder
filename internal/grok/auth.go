package grok

import (
	json "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const maxAuthBytes = 1 << 20

type credentials struct {
	token   string
	account string
	team    string
}

type candidate struct {
	token     string
	scope     string
	account   string
	team      string
	expiresAt string
	authMode  string
	kind      string
}

func resolveAuthFile(explicit string) (string, error) {
	if explicit != "" {
		return filepath.Clean(explicit), nil
	}
	if _, present := os.LookupEnv("GROK_AUTH"); present {
		return "", errors.New("Grok inline authentication is unsupported; use a native authentication file")
	}
	if path, present := os.LookupEnv("GROK_AUTH_JSON"); present {
		if path == "" {
			return "", errors.New("GROK_AUTH_JSON is explicitly empty")
		}
		return filepath.Clean(path), nil
	}
	if path, present := os.LookupEnv("GROK_AUTH_PATH"); present {
		if path == "" {
			return "", errors.New("GROK_AUTH_PATH is explicitly empty")
		}
		return filepath.Clean(path), nil
	}
	if home, present := os.LookupEnv("GROK_HOME"); present {
		if home == "" {
			return "", errors.New("GROK_HOME is explicitly empty")
		}
		return filepath.Join(home, "auth.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", errors.New("Grok authentication directory could not be resolved")
	}
	return filepath.Join(home, ".grok", "auth.json"), nil
}

func openAuthFile(path string) (*os.File, os.FileInfo, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, errors.New("Grok authentication file is missing for profile default")
	}
	if err != nil {
		return nil, nil, errors.New("Grok authentication file cannot be inspected")
	}
	if !info.Mode().IsRegular() || info.Size() < 0 || info.Size() > maxAuthBytes {
		return nil, nil, errors.New("Grok authentication file is not a bounded regular file")
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); ok && stat.Uid != uint32(os.Getuid()) {
		return nil, nil, errors.New("Grok authentication file is not owned by the current user")
	}
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, nil, errors.New("Grok authentication file cannot be read")
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = syscall.Close(fd)
		return nil, nil, errors.New("Grok authentication file cannot be read")
	}
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) || !opened.Mode().IsRegular() || opened.Size() < 0 || opened.Size() > maxAuthBytes {
		_ = file.Close()
		return nil, nil, errors.New("Grok authentication file changed while being opened")
	}
	if stat, ok := opened.Sys().(*syscall.Stat_t); ok && stat.Uid != uint32(os.Getuid()) {
		_ = file.Close()
		return nil, nil, errors.New("Grok authentication file is not owned by the current user")
	}
	return file, opened, nil
}

func readCredentials(path string, now time.Time) (credentials, error) {
	file, before, err := openAuthFile(path)
	if err != nil {
		return credentials{}, err
	}
	defer file.Close()
	if !before.Mode().IsRegular() {
		return credentials{}, errors.New("Grok authentication file changed while being read")
	}
	body, err := io.ReadAll(io.LimitReader(file, maxAuthBytes+1))
	if err != nil || len(body) > maxAuthBytes {
		return credentials{}, errors.New("Grok authentication file cannot be read")
	}
	final, err := file.Stat()
	if err != nil || !os.SameFile(before, final) || !final.Mode().IsRegular() || final.Size() != int64(len(body)) || final.Size() > maxAuthBytes || !final.ModTime().Equal(before.ModTime()) {
		return credentials{}, errors.New("Grok authentication file changed while being read")
	}
	var value any
	if err := json.Unmarshal(body, &value); err != nil {
		return credentials{}, errors.New("Grok authentication file is malformed")
	}
	root, ok := value.(map[string]any)
	if !ok {
		return credentials{}, errors.New("Grok authentication file is malformed")
	}
	candidates := credentialCandidates(root)
	if len(candidates) != 1 {
		if len(candidates) > 1 {
			return credentials{}, errors.New("Grok authentication file contains ambiguous multiple sessions")
		}
		return credentials{}, errors.New("Grok authentication file has no supported consumer session")
	}
	selected := candidates[0]
	if selected.expiresAt != "" {
		expiresAt, err := time.Parse(time.RFC3339Nano, selected.expiresAt)
		if err != nil {
			return credentials{}, errors.New("Grok consumer session expiry is malformed")
		}
		if !expiresAt.After(now) {
			return credentials{}, errors.New("Grok consumer session is expired")
		}
	}
	return credentials{token: selected.token, account: selected.account, team: selected.team}, nil
}

func credentialCandidates(root map[string]any) []candidate {
	if direct, ok := candidateFrom(root, stringValue(root["scope"])); ok {
		if supportedSession(direct) {
			return []candidate{direct}
		}
		return nil
	}
	result := make([]candidate, 0, len(root))
	for scope, raw := range root {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		entry, ok := candidateFrom(item, scope)
		if ok && supportedSession(entry) {
			result = append(result, entry)
		}
	}
	return result
}

func candidateFrom(item map[string]any, fallbackScope string) (candidate, bool) {
	token := stringValue(item["key"])
	if token == "" {
		return candidate{}, false
	}
	scope := firstString(item, "scope", "url", "audience")
	if scope == "" {
		scope = fallbackScope
	}
	return candidate{
		token:     token,
		scope:     scope,
		account:   stringValue(item["email"]),
		team:      firstString(item, "team_id", "teamId"),
		expiresAt: firstString(item, "expires_at", "expiresAt"),
		authMode:  strings.ToLower(firstString(item, "auth_mode", "authMode")),
		kind:      strings.ToLower(firstString(item, "type", "kind")),
	}, true
}

func supportedSession(entry candidate) bool {
	lowered := strings.ToLower(entry.scope)
	if entry.kind == "api-key" || entry.kind == "api_key" || strings.Contains(lowered, "api-key") || strings.Contains(lowered, "api_key") {
		return false
	}
	scope := strings.TrimSpace(entry.scope)
	if before, _, found := strings.Cut(scope, "::"); found {
		scope = before
	}
	if !strings.Contains(scope, "://") {
		scope = "https://" + scope
	}
	parsed, err := url.Parse(scope)
	if err != nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	switch host {
	case "grok.com", "www.grok.com":
		return true
	case "accounts.x.ai":
		return strings.HasPrefix(parsed.Path, "/sign-in")
	case "auth.x.ai":
		return entry.authMode == "oidc"
	default:
		return false
	}
}

func firstString(item map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := stringValue(item[key]); value != "" {
			return value
		}
	}
	return ""
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}

func authIdentity(path string, info os.FileInfo) string {
	identity := fmt.Sprintf("%s\x00%d\x00%d\x00%d", filepath.Clean(path), info.Size(), info.ModTime().UnixNano(), info.Mode())
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		identity += fmt.Sprintf("\x00%d\x00%d", stat.Dev, stat.Ino)
	}
	return identity
}
