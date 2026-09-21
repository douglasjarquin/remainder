package devin

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

const maxAuthBytes = 64 << 10

type credentials struct {
	apiKey  string
	apiHost string
}

func resolveAuthFile(explicit string) (string, error) {
	if explicit != "" {
		return filepath.Clean(explicit), nil
	}
	if path, present := os.LookupEnv("DEVIN_CREDENTIALS"); present {
		if path == "" {
			return "", errors.New("DEVIN_CREDENTIALS is explicitly empty")
		}
		return filepath.Clean(path), nil
	}
	if dir := os.Getenv("XDG_DATA_HOME"); dir != "" {
		return filepath.Join(dir, "devin", "credentials.toml"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", errors.New("Devin credential directory could not be resolved")
	}
	return filepath.Join(home, ".local", "share", "devin", "credentials.toml"), nil
}

func openAuthFile(path string) (*os.File, os.FileInfo, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, errors.New("Devin credential file is missing for profile default")
	}
	if err != nil {
		return nil, nil, errors.New("Devin credential file cannot be inspected")
	}
	if !info.Mode().IsRegular() || info.Size() < 0 || info.Size() > maxAuthBytes {
		return nil, nil, errors.New("Devin credential file is not a bounded regular file")
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); ok && stat.Uid != uint32(os.Getuid()) {
		return nil, nil, errors.New("Devin credential file is not owned by the current user")
	}
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, nil, errors.New("Devin credential file cannot be read")
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = syscall.Close(fd)
		return nil, nil, errors.New("Devin credential file cannot be read")
	}
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) || !opened.Mode().IsRegular() || opened.Size() < 0 || opened.Size() > maxAuthBytes {
		_ = file.Close()
		return nil, nil, errors.New("Devin credential file changed while being opened")
	}
	if stat, ok := opened.Sys().(*syscall.Stat_t); ok && stat.Uid != uint32(os.Getuid()) {
		_ = file.Close()
		return nil, nil, errors.New("Devin credential file is not owned by the current user")
	}
	return file, opened, nil
}

func readCredentials(path string) (credentials, error) {
	file, before, err := openAuthFile(path)
	if err != nil {
		return credentials{}, err
	}
	defer file.Close()
	body, err := io.ReadAll(io.LimitReader(file, maxAuthBytes+1))
	if err != nil || len(body) > maxAuthBytes {
		return credentials{}, errors.New("Devin credential file cannot be read")
	}
	final, err := file.Stat()
	if err != nil || !os.SameFile(before, final) || !final.Mode().IsRegular() || final.Size() != int64(len(body)) || final.Size() > maxAuthBytes || !final.ModTime().Equal(before.ModTime()) {
		return credentials{}, errors.New("Devin credential file changed while being read")
	}
	entries, err := parseCredentials(body)
	if err != nil {
		return credentials{}, err
	}
	apiKey := entries["windsurf_api_key"]
	if apiKey == "" {
		return credentials{}, errors.New("Devin credential file has no API key")
	}
	credential := credentials{apiKey: apiKey}
	if host := entries["api_server_url"]; host != "" {
		parsed, err := url.Parse(host)
		if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil {
			return credentials{}, errors.New("Devin credential file API server URL is invalid")
		}
		credential.apiHost = parsed.Scheme + "://" + parsed.Host
	}
	return credential, nil
}

func parseCredentials(body []byte) (map[string]string, error) {
	entries := make(map[string]string)
	inSection := false
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			rest := strings.TrimSpace(line[strings.Index(line, "]")+1:])
			if !strings.Contains(line, "]") || rest != "" && !strings.HasPrefix(rest, "#") {
				return nil, errors.New("Devin credential file is malformed")
			}
			inSection = true
			continue
		}
		key, raw, found := strings.Cut(line, "=")
		if !found {
			return nil, errors.New("Devin credential file is malformed")
		}
		key = strings.TrimSpace(key)
		raw = strings.TrimSpace(raw)
		if key == "" || strings.ContainsAny(key, " \t\"'[") {
			return nil, errors.New("Devin credential file is malformed")
		}
		if !strings.HasPrefix(raw, `"`) && !strings.HasPrefix(raw, `'`) {
			continue
		}
		value, rest, ok := quotedString(raw)
		if !ok || rest != "" && !strings.HasPrefix(rest, "#") {
			return nil, errors.New("Devin credential file is malformed")
		}
		if inSection {
			continue
		}
		if _, duplicate := entries[key]; duplicate {
			return nil, errors.New("Devin credential file is malformed")
		}
		entries[key] = value
	}
	return entries, nil
}

func quotedString(raw string) (string, string, bool) {
	if strings.HasPrefix(raw, `"`) {
		end := -1
		for index := 1; index < len(raw); index++ {
			if raw[index] == '\\' {
				index++
				continue
			}
			if raw[index] == '"' {
				end = index
				break
			}
		}
		if end < 0 {
			return "", "", false
		}
		value, err := strconv.Unquote(raw[:end+1])
		if err != nil {
			return "", "", false
		}
		return value, strings.TrimSpace(raw[end+1:]), true
	}
	end := strings.Index(raw[1:], `'`)
	if end < 0 {
		return "", "", false
	}
	return raw[1 : end+1], strings.TrimSpace(raw[end+2:]), true
}

func authIdentity(path string, info os.FileInfo) string {
	identity := fmt.Sprintf("%s\x00%d\x00%d\x00%d", filepath.Clean(path), info.Size(), info.ModTime().UnixNano(), info.Mode())
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		identity += fmt.Sprintf("\x00%d\x00%d", stat.Dev, stat.Ino)
	}
	return identity
}
