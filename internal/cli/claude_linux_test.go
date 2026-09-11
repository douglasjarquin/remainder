//go:build linux

package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/douglasjarquin/remainder/internal/cache"
	"github.com/douglasjarquin/remainder/internal/claude"
	"github.com/douglasjarquin/remainder/internal/codex"
)

func TestExecute_native_Claude_missingFileIsOperationalFailure_onLinux(t *testing.T) {
	adapter := runtimeAdapter{codex: codex.Default(), claude: claude.New(claude.Options{AuthFile: filepath.Join(t.TempDir(), "missing")}), newStore: func() (*cache.Store, error) { return cache.NewUserStore(cache.Options{}) }}
	var stdout, stderr bytes.Buffer
	code := executeWithAdapterAt(t.Context(), []string{"value", "--provider", "claude", "--profile", "default", "--window", "weekly", "--field", "remaining", "--cache", "off"}, &stdout, &stderr, "test", fixedCLINow(), adapter)
	if code != 1 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "missing") || strings.Contains(stderr.String(), "Usage:") || strings.Contains(stderr.String(), "--allow-keychain-prompt") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestExecute_native_Claude_allowsKeychainPromptFlagButStaysInert_onLinux(t *testing.T) {
	adapter := runtimeAdapter{codex: codex.Default(), claude: claude.New(claude.Options{AuthFile: filepath.Join(t.TempDir(), "missing")}), newStore: func() (*cache.Store, error) { return cache.NewUserStore(cache.Options{}) }}
	var stdout, stderr bytes.Buffer
	code := executeWithAdapterAt(t.Context(), []string{"value", "--provider", "claude", "--profile", "default", "--window", "weekly", "--field", "remaining", "--cache", "off", "--allow-keychain-prompt"}, &stdout, &stderr, "test", fixedCLINow(), adapter)
	if code != 1 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "missing") || strings.Contains(stderr.String(), "requires --provider") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}
