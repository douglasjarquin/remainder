package cursor

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/douglasjarquin/remainder/internal/cache"
	"github.com/douglasjarquin/remainder/internal/evidence"
)

func TestAdapterMacKeychainSuccess_readsSyntheticTokenAfterConsent(t *testing.T) {
	server := fixtureServer(t, map[string]fixtureResponse{
		"GetCurrentPeriodUsage": {bodyFile: "usage-zero.json"},
		"GetPlanInfo":           {body: `{}`},
		"GetSandUsageStatus":    {body: `{}`},
	})
	defer server.Close()
	configFile := writeMacConfig(t, `{"authInfo":{"email":"person@example.test","userId":"user-1"}}`)
	calls := 0
	adapter := New(Options{
		ConfigFile: configFile,
		KeychainReader: func(context.Context) (string, error) {
			calls++
			return "synthetic-secret\n", nil
		},
		Endpoint: server.URL,
		Client:   server.Client(),
		Now:      func() time.Time { return fixtureNow },
		Timeout:  time.Second,
		goos:     "darwin",
	}).WithKeychainPrompt()

	observation, err := adapter.Observe(t.Context(), cursorRequest())

	if err != nil || calls != 1 {
		t.Fatalf("Observe() error/keychain calls = %v/%d, want success/1", err, calls)
	}
	if observation.Source != (evidence.SourceIdentity{Kind: "native_keychain_http", Name: "cursor_cli_keychain"}) {
		t.Fatalf("source = %+v", observation.Source)
	}
	if observation.Account != (evidence.AccountIdentity{Binding: evidence.IdentityUnknown}) {
		t.Fatalf("account = %+v, want unknown and empty", observation.Account)
	}
}

func TestAdapterMacKeychainConsent_requiredBeforeConfigOrHelper(t *testing.T) {
	calls := 0
	adapter := New(Options{
		ConfigFile: filepath.Join(t.TempDir(), "missing.json"),
		KeychainReader: func(context.Context) (string, error) {
			calls++
			return "synthetic-secret", nil
		},
		goos: "darwin",
	})

	_, err := adapter.Observe(t.Context(), cursorRequest())

	if err == nil || calls != 0 || !strings.Contains(err.Error(), "--allow-keychain-prompt") {
		t.Fatalf("Observe() error/calls = %v/%d", err, calls)
	}
}

func TestAdapterMacCacheBinding_usesConfigIdentityWithoutKeychain(t *testing.T) {
	configFile := writeMacConfig(t, `{"authInfo":{"authId":"user-1","email":"person@example.test"}}`)
	calls := 0
	adapter := New(Options{ConfigFile: configFile, KeychainReader: func(context.Context) (string, error) {
		calls++
		return "synthetic-secret", nil
	}, goos: "darwin"})

	first, firstErr := adapter.CacheBinding(t.Context(), cursorRequest())
	writeMacConfigAt(t, configFile, `{"authInfo":{"authId":"user-2","email":"person@example.test"}}`)
	second, secondErr := adapter.CacheBinding(t.Context(), cursorRequest())

	if firstErr != nil || secondErr != nil || calls != 0 {
		t.Fatalf("CacheBinding() errors/calls = %v/%v/%d", firstErr, secondErr, calls)
	}
	if first.SourceKind != "native_keychain_http" || first.SourceName != "cursor_cli_keychain" || first.CredentialFingerprint == "" || first.CredentialFingerprint == second.CredentialFingerprint {
		t.Fatalf("bindings = %+v / %+v", first, second)
	}
}

func TestAdapterMacKeychainFailures_areBoundedCanceledAndRedacted(t *testing.T) {
	configFile := writeMacConfig(t, `{"authInfo":{"userId":"user-1"}}`)
	tests := []struct {
		name       string
		reader     func(context.Context) (string, error)
		wantPhrase string
		wantKind   cache.FailureKind
	}{
		{name: "empty", reader: func(context.Context) (string, error) { return " \n", nil }, wantPhrase: "empty", wantKind: cache.FailurePermanent},
		{name: "oversized", reader: func(context.Context) (string, error) { return strings.Repeat("x", maxKeychainBytes+1), nil }, wantPhrase: "too large", wantKind: cache.FailurePermanent},
		{name: "helper secret", reader: func(context.Context) (string, error) { return "", errors.New("synthetic-secret") }, wantPhrase: "could not be read", wantKind: cache.FailurePermanent},
		{name: "expired", reader: func(context.Context) (string, error) { return "e30.eyJleHAiOjF9.signature", nil }, wantPhrase: "expired", wantKind: cache.FailurePermanent},
		{name: "deadline", reader: func(ctx context.Context) (string, error) { <-ctx.Done(); return "", ctx.Err() }, wantPhrase: "deadline", wantKind: cache.FailureTransient},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			adapter := New(Options{ConfigFile: configFile, KeychainReader: test.reader, Client: &http.Client{Transport: &countingTransport{}}, Timeout: 10 * time.Millisecond, Now: func() time.Time { return fixtureNow }, goos: "darwin"}).WithKeychainPrompt()

			_, err := adapter.Observe(t.Context(), cursorRequest())
			kind, _ := adapter.Failure(err)

			if err == nil || kind != test.wantKind || !strings.Contains(err.Error(), test.wantPhrase) || strings.Contains(err.Error(), "synthetic-secret") {
				t.Fatalf("Observe() error/kind = %v/%s", err, kind)
			}
		})
	}
}

func TestAdapterMacKeychain_rejectsConfigChangeDuringHelper(t *testing.T) {
	configFile := writeMacConfig(t, `{"authInfo":{"userId":"user-1"}}`)
	transport := &countingTransport{}
	adapter := New(Options{ConfigFile: configFile, KeychainReader: func(context.Context) (string, error) {
		writeMacConfigAt(t, configFile, `{"authInfo":{"userId":"user-2"}}`)
		return "synthetic-secret", nil
	}, Client: &http.Client{Transport: transport}, goos: "darwin"}).WithKeychainPrompt()

	_, err := adapter.Observe(t.Context(), cursorRequest())

	if err == nil || transport.calls != 0 || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("Observe() error/http calls = %v/%d", err, transport.calls)
	}
}

func TestAdapterMacConfig_rejectsUnsafeMalformedAndIdentityFreeFiles(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*testing.T) string
	}{
		{name: "symlink", setup: func(t *testing.T) string {
			target := writeMacConfig(t, `{"authInfo":{"userId":"user-1"}}`)
			path := filepath.Join(t.TempDir(), "config-link.json")
			if err := os.Symlink(target, path); err != nil {
				t.Fatal(err)
			}
			return path
		}},
		{name: "oversized", setup: func(t *testing.T) string { return writeMacConfig(t, strings.Repeat("x", maxAuthBytes+1)) }},
		{name: "malformed", setup: func(t *testing.T) string { return writeMacConfig(t, `{"authInfo":`) }},
		{name: "identity free", setup: func(t *testing.T) string { return writeMacConfig(t, `{"authInfo":{}}`) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			adapter := New(Options{ConfigFile: test.setup(t), KeychainReader: func(context.Context) (string, error) {
				calls++
				return "synthetic-secret", nil
			}, goos: "darwin"}).WithKeychainPrompt()

			_, err := adapter.Observe(t.Context(), cursorRequest())

			if err == nil || calls != 0 {
				t.Fatalf("Observe() error/calls = %v/%d", err, calls)
			}
		})
	}
}

func TestAdapterMacKeychain_rejectsSelectionBeforeConfigOrHelper(t *testing.T) {
	calls := 0
	adapter := New(Options{ConfigFile: filepath.Join(t.TempDir(), "missing.json"), KeychainReader: func(context.Context) (string, error) {
		calls++
		return "synthetic-secret", nil
	}, goos: "darwin"}).WithKeychainPrompt()

	_, err := adapter.Observe(t.Context(), evidence.Request{Provider: "cursor", Profile: "other"})

	if err == nil || calls != 0 || !errors.Is(err, evidence.ErrInvalidSelection) || strings.Contains(err.Error(), "missing") {
		t.Fatalf("Observe() error/calls = %v/%d", err, calls)
	}
}

func TestDefaultMacConfigPath_usesOverrideThenHome(t *testing.T) {
	tests := []struct {
		name    string
		lookup  func(string) (string, bool)
		want    string
		wantErr string
	}{
		{name: "override", lookup: mapLookup(map[string]string{"CURSOR_CLI_CONFIG": "/chosen/cli-config.json", "HOME": "/ignored"}), want: "/chosen/cli-config.json"},
		{name: "home", lookup: mapLookup(map[string]string{"HOME": "/home/person"}), want: "/home/person/.cursor/cli-config.json"},
		{name: "empty override", lookup: mapLookup(map[string]string{"CURSOR_CLI_CONFIG": "", "HOME": "/ignored"}), wantErr: "explicitly empty"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path, err := defaultMacConfigPath(test.lookup)

			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("defaultMacConfigPath() error = %v", err)
				}
				return
			}
			if err != nil || path != test.want {
				t.Fatalf("defaultMacConfigPath() = %q, %v", path, err)
			}
		})
	}
}

func writeMacConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cli-config.json")
	writeMacConfigAt(t, path, body)
	return path
}

func writeMacConfigAt(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func mapLookup(values map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	}
}
