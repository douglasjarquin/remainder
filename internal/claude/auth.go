package claude

import (
	json "encoding/json/v2"
	"errors"
	"io"
	"os"
	"time"
)

type credentials struct{ accessToken string }

type authFile struct {
	OAuth oauthCredentials `json:"claudeAiOauth"`
}

type oauthCredentials struct {
	AccessTokenCamel string       `json:"accessToken"`
	AccessTokenSnake string       `json:"access_token"`
	ExpiresAtCamel   sourceNumber `json:"expiresAt"`
	ExpiresAtSnake   sourceNumber `json:"expires_at"`
}

func inspectAuthFile(path string) (os.FileInfo, error) {
	if path == "" {
		return nil, errors.New("Claude authentication directory could not be resolved")
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, errors.New("Claude authentication file is missing for profile default")
	}
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxAuthBytes {
		return nil, errors.New("Claude authentication file is not a bounded regular file")
	}
	return info, nil
}

func readCredentials(path string, now time.Time) (credentials, error) {
	before, err := inspectAuthFile(path)
	if err != nil {
		return credentials{}, err
	}
	file, err := openAuthFile(path)
	if err != nil {
		return credentials{}, errors.New("Claude authentication file cannot be read")
	}
	defer file.Close()
	after, err := file.Stat()
	if err != nil || !after.Mode().IsRegular() || !os.SameFile(before, after) || after.Size() > maxAuthBytes {
		return credentials{}, errors.New("Claude authentication file is not a bounded regular file")
	}
	body, err := io.ReadAll(io.LimitReader(file, maxAuthBytes+1))
	if err != nil {
		return credentials{}, errors.New("Claude authentication file cannot be read")
	}
	if len(body) > maxAuthBytes {
		return credentials{}, errors.New("Claude authentication file is not a bounded regular file")
	}
	var raw authFile
	if err := json.Unmarshal(body, &raw); err != nil {
		return credentials{}, errors.New("Claude authentication file is malformed")
	}
	token := raw.OAuth.AccessTokenCamel
	if token == "" {
		token = raw.OAuth.AccessTokenSnake
	}
	if token == "" {
		return credentials{}, errors.New("Claude authentication file lacks an OAuth access token")
	}
	expires := raw.OAuth.ExpiresAtCamel
	if expires == "" {
		expires = raw.OAuth.ExpiresAtSnake
	}
	if expires != "" {
		milliseconds, ok := expires.int64()
		if !ok || milliseconds <= 0 {
			return credentials{}, errors.New("Claude OAuth access token expiry is invalid")
		}
		if milliseconds <= now.UnixMilli() {
			return credentials{}, errors.New("Claude OAuth access token is expired")
		}
	}
	return credentials{accessToken: token}, nil
}
