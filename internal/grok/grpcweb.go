package grok

import (
	"encoding/binary"
	"errors"
	"net/url"
	"strconv"
	"strings"
)

func decodeGRPCWeb(body []byte) ([]byte, error) {
	if len(body) == 0 {
		return nil, errors.New("empty gRPC response")
	}
	if body[0] != 0 && body[0] != 1 && body[0]&0x80 == 0 {
		return body, nil
	}
	var payload []byte
	trailerSeen := false
	for offset := 0; offset < len(body); {
		if len(body)-offset < 5 {
			return nil, errors.New("truncated gRPC frame")
		}
		flags := body[offset]
		if flags != 0 && flags != 0x80 {
			return nil, errors.New("unsupported gRPC frame")
		}
		length := uint64(binary.BigEndian.Uint32(body[offset+1 : offset+5]))
		offset += 5
		if length > maxResponseBytes || length > uint64(len(body)-offset) {
			return nil, errors.New("invalid gRPC frame length")
		}
		end := offset + int(length)
		frame := body[offset:end]
		offset = end
		if flags == 0x80 {
			if trailerSeen {
				return nil, errors.New("duplicate gRPC trailers")
			}
			trailerSeen = true
			status, message, err := parseGRPCTrailers(frame)
			if err != nil {
				return nil, err
			}
			if err := grpcStatusError(status, message); err != nil {
				return nil, err
			}
			continue
		}
		if trailerSeen || payload != nil {
			return nil, errors.New("multiple gRPC data frames")
		}
		payload = append([]byte(nil), frame...)
	}
	if payload == nil {
		return nil, errors.New("missing gRPC data frame")
	}
	return payload, nil
}

func parseGRPCTrailers(frame []byte) (string, string, error) {
	if len(frame) > maxResponseBytes {
		return "", "", errors.New("gRPC trailers are too large")
	}
	status := ""
	message := ""
	for line := range strings.SplitSeq(string(frame), "\n") {
		line = strings.TrimSuffix(line, "\r")
		if line == "" {
			continue
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok || name == "" {
			return "", "", errors.New("malformed gRPC trailer")
		}
		switch strings.ToLower(strings.TrimSpace(name)) {
		case "grpc-status":
			if status != "" {
				return "", "", errors.New("duplicate gRPC status")
			}
			status = strings.TrimSpace(value)
		case "grpc-message":
			if message != "" {
				return "", "", errors.New("duplicate gRPC message")
			}
			message = strings.TrimSpace(value)
		}
	}
	return status, message, nil
}

func grpcStatusError(value, message string) error {
	if value == "" || value == "0" {
		return nil
	}
	status, err := strconv.ParseUint(value, 10, 5)
	if err != nil || status > 16 || strconv.FormatUint(status, 10) != value {
		return errors.New("Grok quota response has invalid gRPC status")
	}
	switch status {
	case 16:
		return ErrAuthorizationRejected
	case 7:
		if grpcMessageIsAuthFailure(message) {
			return ErrAuthorizationRejected
		}
		return fmtTransientProtocol()
	case 8:
		return &RetryError{}
	default:
		return fmtTransientProtocol()
	}
}

func grpcMessageIsAuthFailure(message string) bool {
	if message == "" || len(message) > 1024 {
		return false
	}
	decoded, err := url.QueryUnescape(message)
	if err != nil || len(decoded) > 1024 {
		return false
	}
	lowered := strings.ToLower(decoded)
	return strings.Contains(lowered, "unauthenticated") ||
		strings.Contains(lowered, "authentication required") ||
		strings.Contains(lowered, "sign-in required") ||
		strings.Contains(lowered, "sign in required") ||
		strings.Contains(lowered, "bad credentials") ||
		strings.Contains(lowered, "token expired") ||
		strings.Contains(lowered, "token invalid") ||
		strings.Contains(lowered, "invalid token") ||
		strings.Contains(lowered, "revoked token") ||
		strings.Contains(lowered, "token revoked") ||
		strings.Contains(lowered, "credentials invalid") ||
		strings.Contains(lowered, "invalid credentials") ||
		strings.Contains(lowered, "credentials revoked") ||
		strings.Contains(lowered, "token could not be validated") ||
		strings.Contains(lowered, "oauth2 could not be validated")
}

func fmtTransientProtocol() error {
	return errors.Join(ErrTransient, errors.New("Grok quota RPC failed"))
}
