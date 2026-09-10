//go:build darwin

package cli

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/douglasjarquin/remainder/internal/cache"
	"github.com/douglasjarquin/remainder/internal/claude"
)

func TestExecute_native_Claude_requiresPromptConsentBeforeSourceAccess_onDarwin(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	var stdout, stderr bytes.Buffer

	code := Execute(t.Context(), []string{"--provider=claude", "--profile=default", "--cache=off"}, &stdout, &stderr, "test")

	if code != 1 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "--allow-keychain-prompt") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestExecute_native_Claude_promptedKeychainThenCachedOnlySkipsHelper_onDarwin(t *testing.T) {
	now := fixedCLINow()
	var keychainCalls atomic.Int32
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if strings.HasSuffix(r.URL.Path, "/profile") {
			fmt.Fprint(w, `{"account":{"uuid":"account-1"}}`)
			return
		}
		fmt.Fprint(w, `{"five_hour":{"utilization":0}}`)
	}))
	defer server.Close()
	provider := claude.New(claude.Options{
		AuthFile:        filepath.Join(t.TempDir(), "missing", ".credentials.json"),
		ProfileEndpoint: server.URL + "/profile",
		UsageEndpoint:   server.URL + "/usage",
		KeychainReader: func(context.Context) ([]byte, error) {
			keychainCalls.Add(1)
			return []byte(`{"claudeAiOauth":{"accessToken":"synthetic-access","expiresAt":1788973200000}}`), nil
		},
		Client: server.Client(),
		Now:    func() time.Time { return now },
	})
	store := cache.New(filepath.Join(t.TempDir(), "cache"), cache.Options{Now: func() time.Time { return now }})
	adapter := runtimeAdapter{claude: provider, newStore: func() (*cache.Store, error) { return store, nil }}
	var stdout, stderr bytes.Buffer

	firstCode := executeWithAdapterAt(t.Context(), []string{"--provider=claude", "--profile=default", "--allow-keychain-prompt"}, &stdout, &stderr, "test", now, adapter)
	stdout.Reset()
	secondCode := executeWithAdapterAt(t.Context(), []string{"--provider=claude", "--profile=default", "--cache=only", "--max-age=1m", "--format=json"}, &stdout, &stderr, "test", now, adapter)
	cachedOutput := stdout.String()
	stdout.Reset()
	thirdCode := executeWithAdapterAt(t.Context(), []string{"--provider=claude", "--profile=default", "--cache=off"}, &stdout, &stderr, "test", now, adapter)

	if firstCode != 0 || secondCode != 0 || thirdCode != 1 || keychainCalls.Load() != 1 || requests.Load() != 4 || !strings.Contains(stderr.String(), "--allow-keychain-prompt") {
		t.Fatalf("codes=%d/%d/%d keychain=%d requests=%d stderr=%q", firstCode, secondCode, thirdCode, keychainCalls.Load(), requests.Load(), stderr.String())
	}
	if !strings.Contains(cachedOutput, `"source":{"kind":"native_keychain_http","name":"claude_keychain"}`) {
		t.Fatalf("cached stdout=%q", cachedOutput)
	}
}
