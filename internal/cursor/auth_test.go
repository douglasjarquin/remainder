package cursor

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/douglasjarquin/remainder/internal/cache"
	"github.com/douglasjarquin/remainder/internal/evidence"
)

func TestDefault_resolvesLinuxAuthFilePrecedence(t *testing.T) {
	tests := []struct {
		name       string
		cursorSet  bool
		cursor     string
		xdg        string
		home       string
		wantSuffix string
		wantErr    string
	}{
		{name: "explicit override", cursorSet: true, cursor: "/chosen/auth.json", xdg: "/ignored", home: "/home/ignored", wantSuffix: "/chosen/auth.json"},
		{name: "empty override is invalid", cursorSet: true, cursor: "", xdg: "/ignored", home: "/home/ignored", wantErr: "empty"},
		{name: "XDG", xdg: "/xdg", home: "/home/ignored", wantSuffix: "/xdg/cursor/auth.json"},
		{name: "HOME", home: "/home/test", wantSuffix: "/home/test/.config/cursor/auth.json"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Given
			getenv := func(key string) (string, bool) {
				switch key {
				case "CURSOR_CLI_CONFIG":
					return test.cursor, test.cursorSet
				case "XDG_CONFIG_HOME":
					return test.xdg, test.xdg != ""
				case "HOME":
					return test.home, test.home != ""
				default:
					return "", false
				}
			}

			// When
			adapter := defaultWithRuntime("linux", getenv)

			// Then
			if test.wantErr != "" {
				_, err := adapter.CacheBinding(t.Context(), cursorRequest())
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("CacheBinding() error = %v", err)
				}
				return
			}
			if adapter.authFile != test.wantSuffix {
				t.Fatalf("auth file = %q, want %q", adapter.authFile, test.wantSuffix)
			}
		})
	}
}

func TestAdapterAuth_rejectsUnsafeOrInvalidFiles(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*testing.T) string
		want  string
	}{
		{name: "missing", setup: func(t *testing.T) string { return filepath.Join(t.TempDir(), "missing") }, want: "missing"},
		{name: "empty token", setup: func(t *testing.T) string { return writeAuth(t, "   ") }, want: "accessToken"},
		{name: "refresh token only", setup: func(t *testing.T) string { return writeRawAuth(t, `{"refreshToken":"must-not-be-read"}`) }, want: "accessToken"},
		{name: "malformed", setup: func(t *testing.T) string { return writeRawAuth(t, `{"accessToken":`) }, want: "malformed"},
		{name: "expired JWT", setup: func(t *testing.T) string { return writeAuth(t, `header.eyJleHAiOjF9.signature`) }, want: "expired"},
		{name: "oversized", setup: func(t *testing.T) string { return writeRawAuth(t, strings.Repeat("x", maxAuthBytes+1)) }, want: "too large"},
		{name: "symlink", setup: func(t *testing.T) string {
			target := writeAuth(t, "secret")
			path := filepath.Join(t.TempDir(), "auth.json")
			if err := os.Symlink(target, path); err != nil {
				t.Fatal(err)
			}
			return path
		}, want: "regular"},
		{name: "fifo", setup: func(t *testing.T) string {
			path := filepath.Join(t.TempDir(), "auth.json")
			if err := syscall.Mkfifo(path, 0o600); err != nil {
				t.Fatal(err)
			}
			return path
		}, want: "regular"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Given
			adapter := New(Options{AuthFile: test.setup(t), Endpoint: "http://127.0.0.1:1", Client: http.DefaultClient, goos: "linux", Now: func() time.Time { return fixtureNow }})

			// When
			_, err := adapter.Observe(t.Context(), cursorRequest())

			// Then
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Observe() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestAdapterObserve_acceptsOpaqueTokenWithoutInventingExpiry(t *testing.T) {
	// Given
	server := fixtureServer(t, map[string]fixtureResponse{
		"GetCurrentPeriodUsage": {bodyFile: "usage-zero.json"},
		"GetPlanInfo":           {body: `{}`},
		"GetSandUsageStatus":    {body: `{}`},
	})
	defer server.Close()
	adapter := New(Options{AuthFile: writeAuth(t, "opaque-token"), Endpoint: server.URL, Client: server.Client(), goos: "linux", Now: func() time.Time { return fixtureNow }})

	// When
	_, err := adapter.Observe(t.Context(), cursorRequest())
	// Then
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
}

func TestAdapterUnsupportedOS_refusesBeforeFileOrHTTPAccess(t *testing.T) {
	// Given
	transport := &countingTransport{}
	adapter := New(Options{AuthFile: writeAuth(t, "synthetic-secret"), Client: &http.Client{Transport: transport}, goos: "darwin"})

	// When
	_, observeErr := adapter.Observe(t.Context(), cursorRequest())
	_, bindingErr := adapter.CacheBinding(t.Context(), cursorRequest())

	// Then
	if !errors.Is(observeErr, ErrUnsupported) || !errors.Is(bindingErr, ErrUnsupported) || transport.calls != 0 {
		t.Fatalf("errors/calls = %v/%v/%d", observeErr, bindingErr, transport.calls)
	}
}

func TestAdapterCacheBinding_usesMetadataOnly(t *testing.T) {
	// Given
	path := writeRawAuth(t, "not JSON and synthetic-secret")
	adapter := New(Options{AuthFile: path, goos: "linux"})

	// When
	binding, err := adapter.CacheBinding(t.Context(), cursorRequest())

	// Then
	if err != nil || binding.Provider != "cursor" || binding.Profile != "default" || binding.ResponseBoundary != "usage" || binding.SourceKind != "native_file_http" || binding.SourceName != "cursor_cli_auth_json" || binding.CredentialFingerprint == "" {
		t.Fatalf("binding = %+v, error = %v", binding, err)
	}
	if _, observeErr := adapter.Observe(t.Context(), cursorRequest()); observeErr == nil {
		t.Fatal("Observe() accepted metadata-only malformed fixture")
	}
}

func TestAdapterFailure_classifiesSelectionAndCancellation(t *testing.T) {
	adapter := Adapter{}
	tests := []struct {
		name string
		err  error
		want cache.FailureKind
	}{
		{name: "wrong account", err: evidence.ErrWrongAccount, want: cache.FailureAccountMismatch},
		{name: "canceled", err: contextCanceled(), want: cache.FailureTransient},
		{name: "invalid auth", err: ErrInvalidAuth, want: cache.FailurePermanent},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			kind, _ := adapter.Failure(test.err)
			if kind != test.want {
				t.Fatalf("Adapter.Failure() = %s, want %s", kind, test.want)
			}
		})
	}
}

func TestAuthFileMetadata_rejectsUnownedAndUnstableFiles(t *testing.T) {
	// Given
	uid := os.Getuid()
	before := syntheticAuthFileInfo{size: 20, modified: fixtureNow, stat: syscall.Stat_t{Uid: uint32(uid), Dev: 1, Ino: 2}}
	tests := []struct {
		name  string
		after syntheticAuthFileInfo
	}{
		{name: "unowned", after: syntheticAuthFileInfo{size: 20, modified: fixtureNow, stat: syscall.Stat_t{Uid: uint32(uid + 1), Dev: 1, Ino: 2}}},
		{name: "different file", after: syntheticAuthFileInfo{size: 20, modified: fixtureNow, stat: syscall.Stat_t{Uid: uint32(uid), Dev: 1, Ino: 3}}},
		{name: "changed size", after: syntheticAuthFileInfo{size: 19, modified: fixtureNow, stat: syscall.Stat_t{Uid: uint32(uid), Dev: 1, Ino: 2}}},
		{name: "changed modification time", after: syntheticAuthFileInfo{size: 20, modified: fixtureNow.Add(time.Second), stat: syscall.Stat_t{Uid: uint32(uid), Dev: 1, Ino: 2}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// When
			stable := authFileStable(before, test.after, 20, uid)

			// Then
			if stable {
				t.Fatal("authFileStable() accepted unsafe metadata")
			}
		})
	}
}

func writeRawAuth(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "auth.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func contextCanceled() error {
	return fmt.Errorf("wrapped: %w", context.Canceled)
}

type syntheticAuthFileInfo struct {
	size     int64
	modified time.Time
	stat     syscall.Stat_t
}

func (i syntheticAuthFileInfo) Name() string       { return "auth.json" }
func (i syntheticAuthFileInfo) Size() int64        { return i.size }
func (i syntheticAuthFileInfo) Mode() os.FileMode  { return 0o600 }
func (i syntheticAuthFileInfo) ModTime() time.Time { return i.modified }
func (i syntheticAuthFileInfo) IsDir() bool        { return false }
func (i syntheticAuthFileInfo) Sys() any           { return &i.stat }
