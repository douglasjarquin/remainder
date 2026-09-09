package codex

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/douglasjarquin/remainder/internal/evidence"
)

func TestAdapterObserve_normalizesCodexWindows(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer synthetic-secret" || r.Header.Get("ChatGPT-Account-Id") != "acct-test" {
			t.Fatal("request did not carry the selected synthetic identity")
		}
		fmt.Fprint(w, `{"account_id":"acct-test","rate_limit":{"primary_window":{"used_percent":100,"limit_window_seconds":18000,"reset_at":"2026-03-08T20:00:00Z"},"secondary_window":{"used_percent":"20","limit_window_seconds":"604800"}},"code_review_rate_limit":{"primary_window":{"used_percent":10,"limit_window_seconds":18000}},"additional_rate_limits":[{"metered_feature":"gpt-test","limit_name":"Test model","rate_limit":{"primary_window":{"used_percent":25,"limit_window_seconds":3600}}}],"credits":{"balance":"7"}}`)
	}))
	defer server.Close()

	observation, err := newTestAdapter(t, server.Client(), []string{server.URL}).Observe(t.Context(), evidence.Request{Provider: "codex", Profile: "default"})
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	if observation.Account.LastObserved != "acct-test" || observation.Account.Binding != evidence.IdentityVerified {
		t.Fatalf("account = %+v, want verified selected account", observation.Account)
	}
	if len(observation.Windows) != 7 {
		t.Fatalf("windows = %d, want known and unknown base, review, model, and credits windows", len(observation.Windows))
	}
	remaining, err := evidence.SelectValue(observation, evidence.ValueRequest{Provider: "codex", Profile: "default", Window: "five_hour", Field: evidence.FieldRemaining}, evidence.FreshOnly)
	if err != nil || remaining != "0\n" {
		t.Fatalf("five-hour remaining = %q, %v, want exhausted zero", remaining, err)
	}
	model, err := evidence.SelectValue(observation, evidence.ValueRequest{Provider: "codex", Profile: "default", Window: "model_gpt-test_window_3600", Field: evidence.FieldRemaining}, evidence.FreshOnly)
	if err != nil || model != "75\n" {
		t.Fatalf("model remaining = %q, %v, want 75", model, err)
	}
}

func TestAdapterObserve_rejectsInvalidEvidenceSafely(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		request evidence.Request
		want    string
	}{
		{name: "wrong profile", body: `{}`, request: evidence.Request{Provider: "codex", Profile: "other"}, want: "profile"},
		{name: "wrong account", body: `{"account_id":"other","rate_limit":{"primary_window":{"used_percent":1}}}`, request: evidence.Request{Provider: "codex", Profile: "default"}, want: "account"},
		{name: "invalid percent", body: `{"account_id":"acct-test","rate_limit":{"primary_window":{"used_percent":101}}}`, request: evidence.Request{Provider: "codex", Profile: "default"}, want: "percentage"},
		{name: "schema drift", body: `{"account_id":"acct-test","new_limits":{}}`, request: evidence.Request{Provider: "codex", Profile: "default"}, want: "schema"},
		{name: "non JSON", body: `upstream exploded synthetic-secret`, request: evidence.Request{Provider: "codex", Profile: "default"}, want: "malformed"},
		{name: "unauthorized", status: http.StatusUnauthorized, request: evidence.Request{Provider: "codex", Profile: "default"}, want: "authentication"},
		{name: "forbidden", status: http.StatusForbidden, request: evidence.Request{Provider: "codex", Profile: "default"}, want: "authentication"},
		{name: "rate limited", status: http.StatusTooManyRequests, request: evidence.Request{Provider: "codex", Profile: "default"}, want: "rate limited"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if test.status != 0 {
					if test.status == http.StatusTooManyRequests {
						w.Header().Set("Retry-After", "synthetic-secret")
					}
					w.WriteHeader(test.status)
				}
				fmt.Fprint(w, test.body)
			}))
			defer server.Close()
			_, err := newTestAdapter(t, server.Client(), []string{server.URL}).Observe(t.Context(), test.request)
			if err == nil || !strings.Contains(err.Error(), test.want) || strings.Contains(err.Error(), "synthetic-secret") {
				t.Fatalf("Observe() error = %v, want safe %q diagnostic", err, test.want)
			}
		})
	}
}

func TestAdapterObserve_boundsSources(t *testing.T) {
	t.Run("malformed auth", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "auth.json")
		if err := os.WriteFile(path, []byte(`{"tokens":`), 0o600); err != nil {
			t.Fatal(err)
		}
		adapter := New(Options{AuthFile: path, Endpoints: []string{"http://127.0.0.1"}, Client: http.DefaultClient, Timeout: time.Second})
		_, err := adapter.Observe(t.Context(), evidence.Request{Provider: "codex", Profile: "default"})
		if err == nil || !strings.Contains(err.Error(), "malformed") {
			t.Fatalf("Observe() error = %v", err)
		}
	})

	t.Run("missing auth", func(t *testing.T) {
		adapter := New(Options{AuthFile: filepath.Join(t.TempDir(), "missing"), Endpoints: []string{"http://127.0.0.1"}, Client: http.DefaultClient, Timeout: time.Second})
		_, err := adapter.Observe(t.Context(), evidence.Request{Provider: "codex", Profile: "default"})
		if err == nil || !strings.Contains(err.Error(), "authentication file is missing") {
			t.Fatalf("Observe() error = %v", err)
		}
	})

	t.Run("expired auth", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "auth.json")
		if err := os.WriteFile(path, []byte(`{"tokens":{"access_token":"header.eyJleHAiOjF9.signature","account_id":"acct-test"}}`), 0o600); err != nil {
			t.Fatal(err)
		}
		adapter := New(Options{AuthFile: path, Endpoints: []string{"http://127.0.0.1"}, Client: http.DefaultClient, Timeout: time.Second, Now: func() time.Time { return time.Unix(2, 0) }})
		_, err := adapter.Observe(t.Context(), evidence.Request{Provider: "codex", Profile: "default"})
		if err == nil || !strings.Contains(err.Error(), "expired") {
			t.Fatalf("Observe() error = %v", err)
		}
	})

	t.Run("oversized response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, strings.Repeat("x", maxResponseBytes+1)) }))
		defer server.Close()
		_, err := newTestAdapter(t, server.Client(), []string{server.URL}).Observe(t.Context(), evidence.Request{Provider: "codex", Profile: "default"})
		if err == nil || !strings.Contains(err.Error(), "too large") {
			t.Fatalf("Observe() error = %v", err)
		}
	})

	t.Run("redirect does not leak", func(t *testing.T) {
		var leaked bool
		destination := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { leaked = r.Header.Get("Authorization") != "" }))
		defer destination.Close()
		source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, http.StatusFound) }))
		defer source.Close()
		_, _ = newTestAdapter(t, source.Client(), []string{source.URL}).Observe(t.Context(), evidence.Request{Provider: "codex", Profile: "default"})
		if leaked {
			t.Fatal("authorization header reached redirect destination")
		}
	})

	t.Run("cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		_, err := newTestAdapter(t, http.DefaultClient, []string{"http://127.0.0.1"}).Observe(ctx, evidence.Request{Provider: "codex", Profile: "default"})
		if err == nil || !strings.Contains(err.Error(), "canceled") {
			t.Fatalf("Observe() error = %v", err)
		}
	})

	t.Run("total deadline", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
		defer server.Close()
		adapter := New(Options{AuthFile: writeTestAuth(t), Endpoints: []string{server.URL}, Client: server.Client(), Timeout: 20 * time.Millisecond})
		_, err := adapter.Observe(t.Context(), evidence.Request{Provider: "codex", Profile: "default"})
		if err == nil || !strings.Contains(err.Error(), "timed out") {
			t.Fatalf("Observe() error = %v", err)
		}
	})

	t.Run("authentication rejection retries second endpoint", func(t *testing.T) {
		calls := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			calls++
			if calls == 1 {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			fmt.Fprint(w, `{"account_id":"acct-test","rate_limit":{"primary_window":{"used_percent":1}}}`)
		}))
		defer server.Close()
		adapter := New(Options{AuthFile: writeTestAuth(t), Endpoints: []string{server.URL + "/first", server.URL + "/second"}, Client: server.Client(), Timeout: time.Second})
		if _, err := adapter.Observe(t.Context(), evidence.Request{Provider: "codex", Profile: "default"}); err != nil || calls != 2 {
			t.Fatalf("Observe() error = %v, calls = %d, want successful second endpoint", err, calls)
		}
	})

	t.Run("account mismatch does not fall back", func(t *testing.T) {
		calls := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			calls++
			fmt.Fprint(w, `{"account_id":"other","rate_limit":{"primary_window":{"used_percent":1}}}`)
		}))
		defer server.Close()
		adapter := New(Options{AuthFile: writeTestAuth(t), Endpoints: []string{server.URL + "/first", server.URL + "/second"}, Client: server.Client(), Timeout: time.Second})
		_, err := adapter.Observe(t.Context(), evidence.Request{Provider: "codex", Profile: "default"})
		if err == nil || calls != 1 {
			t.Fatalf("Observe() error = %v, calls = %d, want immediate mismatch refusal", err, calls)
		}
	})
}

func newTestAdapter(t *testing.T, client *http.Client, endpoints []string) Adapter {
	t.Helper()
	return New(Options{AuthFile: writeTestAuth(t), Endpoints: endpoints, Client: client, Timeout: time.Second, Now: func() time.Time { return time.Unix(1_773_000_000, 0).UTC() }})
}

func writeTestAuth(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "auth.json")
	if err := os.WriteFile(path, []byte(`{"tokens":{"access_token":"synthetic-secret","account_id":"acct-test"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
