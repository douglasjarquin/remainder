//go:build darwin

package cli

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/douglasjarquin/remainder/internal/cache"
	"github.com/douglasjarquin/remainder/internal/cursor"
	"github.com/douglasjarquin/remainder/internal/evidence"
)

func TestExecute_native_Cursor_requiresPromptConsentBeforeSourceAccess_onDarwin(t *testing.T) {
	t.Setenv("CURSOR_CLI_CONFIG", t.TempDir()+"/missing-auth.json")
	var stdout, stderr bytes.Buffer

	code := Execute(t.Context(), []string{"--provider=cursor", "--profile=default", "--cache=off"}, &stdout, &stderr, "test")

	if code != 1 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "--allow-keychain-prompt") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestAllRequests_includeCursorLast_onDarwin(t *testing.T) {
	want := []evidence.Provider{"codex", "claude", "grok", "cursor"}
	if len(allRequests) != len(want) {
		t.Fatalf("allRequests=%+v", allRequests)
	}
	for index, request := range allRequests {
		if request.Provider != want[index] || request.Profile != "default" {
			t.Fatalf("allRequests[%d]=%+v, want %s/default", index, request, want[index])
		}
	}
}

func TestExecute_native_Cursor_rejectsInvalidProfileBeforePromptConsent_onDarwin(t *testing.T) {
	t.Setenv("CURSOR_CLI_CONFIG", t.TempDir()+"/missing-auth.json")
	var stdout, stderr bytes.Buffer

	code := Execute(t.Context(), []string{"--provider=cursor", "--profile=other", "--cache=off"}, &stdout, &stderr, "test")

	if code != 2 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "requires --provider cursor --profile default") || strings.Contains(stderr.String(), "--allow-keychain-prompt") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestExecute_native_Cursor_promptedRefreshThenCachedOnlySkipsKeychain_onDarwin(t *testing.T) {
	now := fixedCLINow()
	configFile := filepath.Join(t.TempDir(), "cli-config.json")
	if err := os.WriteFile(configFile, []byte(`{"authInfo":{"userId":"user-1"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var keychainCalls atomic.Int32
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		switch filepath.Base(request.URL.Path) {
		case "GetCurrentPeriodUsage":
			body, err := os.ReadFile(filepath.Join("..", "cursor", "testdata", "usage-zero.json"))
			if err != nil {
				t.Fatal(err)
			}
			fmt.Fprint(w, string(body))
		case "GetPlanInfo", "GetSandUsageStatus":
			fmt.Fprint(w, `{}`)
		default:
			t.Fatalf("unexpected request %s", request.URL.Path)
		}
	}))
	defer server.Close()
	provider := cursor.New(cursor.Options{
		ConfigFile: configFile,
		KeychainReader: func(context.Context) (string, error) {
			keychainCalls.Add(1)
			return "synthetic-secret", nil
		},
		Endpoint: server.URL,
		Client:   server.Client(),
		Now:      func() time.Time { return now },
	})
	store := cache.New(filepath.Join(t.TempDir(), "cache"), cache.Options{Now: func() time.Time { return now }})
	adapter := runtimeAdapter{cursor: provider, newStore: func() (*cache.Store, error) { return store, nil }}
	var stdout, stderr bytes.Buffer

	firstCode := executeWithAdapterAt(t.Context(), []string{"--provider=cursor", "--profile=default", "--allow-keychain-prompt"}, &stdout, &stderr, "test", now, adapter)
	stdout.Reset()
	secondCode := executeWithAdapterAt(t.Context(), []string{"--provider=cursor", "--profile=default", "--cache=only", "--max-age=1m", "--format=json"}, &stdout, &stderr, "test", now, adapter)
	cachedOutput := stdout.String()
	stdout.Reset()
	thirdCode := executeWithAdapterAt(t.Context(), []string{"--provider=cursor", "--profile=default", "--cache=off"}, &stdout, &stderr, "test", now, adapter)

	if firstCode != 0 || secondCode != 0 || thirdCode != 1 || keychainCalls.Load() != 1 || requests.Load() != 3 || !strings.Contains(stderr.String(), "--allow-keychain-prompt") {
		t.Fatalf("codes=%d/%d/%d keychain=%d requests=%d stderr=%q", firstCode, secondCode, thirdCode, keychainCalls.Load(), requests.Load(), stderr.String())
	}
	if !strings.Contains(cachedOutput, `"source":{"kind":"native_keychain_http","name":"cursor_cli_keychain"}`) || !strings.Contains(cachedOutput, `"account":{"last_observed":"","binding":"unknown"}`) {
		t.Fatalf("cached stdout=%q", cachedOutput)
	}
}
