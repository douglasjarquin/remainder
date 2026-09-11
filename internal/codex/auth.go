package codex

import (
	"encoding/base64"
	json "encoding/json/v2"
	"errors"
	"io"
	"os"
	"strings"
	"time"
)

const maxAuthBytes = 1 << 20

type credentials struct {
	accessToken string
	accountID   string
}

type authFile struct {
	Tokens authTokens `json:"tokens"`
}

type authTokens struct {
	AccessTokenSnake string `json:"access_token"`
	AccessTokenCamel string `json:"accessToken"`
	IDTokenSnake     string `json:"id_token"`
	IDTokenCamel     string `json:"idToken"`
	AccountIDSnake   string `json:"account_id"`
	AccountIDCamel   string `json:"accountId"`
}

func readCredentials(path string, now time.Time) (credentials, error) {
	if path == "" {
		return credentials{}, errors.New("Codex authentication directory could not be resolved")
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return credentials{}, errors.New("Codex authentication file is missing for profile default")
	}
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxAuthBytes {
		return credentials{}, errors.New("Codex authentication file is not a bounded regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return credentials{}, errors.New("Codex authentication file cannot be read")
	}
	defer file.Close()
	var raw authFile
	if err := json.UnmarshalRead(io.LimitReader(file, maxAuthBytes+1), &raw); err != nil {
		return credentials{}, errors.New("Codex authentication file is malformed")
	}
	token := raw.Tokens.AccessTokenSnake
	if token == "" {
		token = raw.Tokens.AccessTokenCamel
	}
	accountID := raw.Tokens.AccountIDSnake
	if accountID == "" {
		accountID = raw.Tokens.AccountIDCamel
	}
	accessClaims := decodeClaims(token)
	idToken := raw.Tokens.IDTokenSnake
	if idToken == "" {
		idToken = raw.Tokens.IDTokenCamel
	}
	identityClaims := decodeClaims(idToken)
	if accountID == "" {
		accountID = identityClaims.AccountID
	}
	if accountID == "" {
		accountID = accessClaims.AccountID
	}
	if token == "" || accountID == "" {
		return credentials{}, errors.New("Codex authentication file lacks OAuth account binding")
	}
	if accessClaims.ExpiresAt != 0 && accessClaims.ExpiresAt <= now.Unix() {
		return credentials{}, errors.New("Codex OAuth access token is expired")
	}
	return credentials{accessToken: token, accountID: accountID}, nil
}

type tokenClaims struct {
	ExpiresAt int64  `json:"exp"`
	AccountID string `json:"https://api.openai.com/auth/account_id"`
}

func decodeClaims(token string) tokenClaims {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return tokenClaims{}
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return tokenClaims{}
	}
	var claims tokenClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return tokenClaims{}
	}
	return claims
}
