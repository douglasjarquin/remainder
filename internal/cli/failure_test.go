package cli

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/douglasjarquin/remainder/internal/cache"
	"github.com/douglasjarquin/remainder/internal/claude"
	"github.com/douglasjarquin/remainder/internal/codex"
	"github.com/douglasjarquin/remainder/internal/evidence"
)

func TestExecute_accountSelectorMismatchExitsTwo(t *testing.T) {
	now := fixedCLINow()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/profile") {
			_, _ = w.Write([]byte(`{"account":{"uuid":"account-1"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"account_id":"acct-test","rate_limit":{"primary_window":{"used_percent":1}}}`))
	}))
	t.Cleanup(server.Close)
	for _, test := range []struct {
		provider string
		account  string
	}{
		{provider: "codex", account: "acct-other"},
		{provider: "claude", account: "account-2"},
	} {
		for _, cacheMode := range []string{"off", "auto"} {
			t.Run(test.provider+"/"+cacheMode, func(t *testing.T) {
				adapter := failureRuntimeAdapter(t, server, now)
				var stdout, stderr bytes.Buffer

				code := executeWithAdapterAt(t.Context(), []string{"--provider=" + test.provider, "--profile=default", "--account=" + test.account, "--cache=" + cacheMode}, &stdout, &stderr, "test", now, adapter)

				if code != 2 || stdout.Len() != 0 || !strings.Contains(stderr.String(), evidence.ErrWrongAccount.Error()) {
					t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
				}
			})
		}
	}
}

func TestExecute_codexResponseAccountMismatchRevokesCachedEvidence(t *testing.T) {
	now := fixedCLINow()
	var account atomic.Value
	account.Store("acct-test")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"account_id":"` + account.Load().(string) + `","rate_limit":{"primary_window":{"used_percent":1}}}`))
	}))
	t.Cleanup(server.Close)
	adapter := failureRuntimeAdapter(t, server, now)
	var stdout, stderr bytes.Buffer
	if code := executeWithAdapterAt(t.Context(), []string{"--provider=codex", "--profile=default"}, &stdout, &stderr, "test", now, adapter); code != 0 {
		t.Fatalf("seed code=%d stderr=%q", code, stderr.String())
	}
	account.Store("acct-other")
	stdout.Reset()
	stderr.Reset()

	code := executeWithAdapterAt(t.Context(), []string{"--provider=codex", "--profile=default", "--refresh", "--stale-on-error"}, &stdout, &stderr, "test", now, adapter)

	if code != 1 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "account does not match the credential") {
		t.Fatalf("mismatch code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	code = executeWithAdapterAt(t.Context(), []string{"--provider=codex", "--profile=default", "--cache=only", "--max-age=1h"}, &stdout, &stderr, "test", now, adapter)
	if code != 1 || stdout.Len() != 0 || !strings.Contains(stderr.String(), cache.ErrUnavailable.Error()) {
		t.Fatalf("cached-only code=%d stdout=%q stderr=%q, want revoked record", code, stdout.String(), stderr.String())
	}
}

func failureRuntimeAdapter(t *testing.T, server *httptest.Server, now time.Time) runtimeAdapter {
	t.Helper()
	directory := t.TempDir()
	codexAuth := filepath.Join(directory, "auth.json")
	claudeAuth := filepath.Join(directory, ".credentials.json")
	if err := os.WriteFile(codexAuth, []byte(`{"tokens":{"access_token":"synthetic-secret","account_id":"acct-test"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(claudeAuth, []byte(`{"claudeAiOauth":{"accessToken":"synthetic-access","expiresAt":1788973200000}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	clock := func() time.Time { return now }
	store := cache.New(filepath.Join(directory, "cache"), cache.Options{Now: clock})
	return runtimeAdapter{
		codex:    codex.New(codex.Options{AuthFile: codexAuth, Endpoints: []string{server.URL}, Client: server.Client(), Timeout: time.Second, Now: clock}),
		claude:   claude.New(claude.Options{AuthFile: claudeAuth, ProfileEndpoint: server.URL + "/profile", UsageEndpoint: server.URL + "/usage", Client: server.Client(), Now: clock}),
		newStore: func() (*cache.Store, error) { return store, nil },
	}
}
