package cursor

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"github.com/douglasjarquin/remainder/internal/cache"
	"github.com/douglasjarquin/remainder/internal/evidence"
)

func (a Adapter) CacheBinding(ctx context.Context, request evidence.Request) (cache.Binding, error) {
	if err := a.validateRequest(ctx, request); err != nil {
		return cache.Binding{}, err
	}
	if a.goos == "darwin" {
		snapshot, err := readMacConfig(a.configFile)
		if err != nil {
			return cache.Binding{}, collectionError(err)
		}
		return cache.Binding{Provider: "cursor", Profile: "default", ResponseBoundary: "usage", SourceKind: "native_keychain_http", SourceName: "cursor_cli_keychain", CredentialFingerprint: snapshot.fingerprint}, nil
	}
	info, err := inspectAuthFile(a.authFile)
	if err != nil {
		return cache.Binding{}, collectionError(err)
	}
	return cache.Binding{Provider: "cursor", Profile: "default", ResponseBoundary: "usage", SourceKind: "native_file_http", SourceName: "cursor_cli_auth_json", CredentialFingerprint: fileFingerprint(a.authFile, info, "")}, nil
}

func fileFingerprint(path string, info os.FileInfo, identity string) string {
	value := fmt.Sprintf("%s\x00%d\x00%d\x00%d", filepath.Clean(path), info.Size(), info.ModTime().UnixNano(), info.Mode())
	if identity != "" {
		value += "\x00" + identity
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		value += fmt.Sprintf("\x00%d\x00%d", stat.Dev, stat.Ino)
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(value)))
}
