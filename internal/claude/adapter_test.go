package claude

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/douglasjarquin/remainder/internal/cache"
	"github.com/douglasjarquin/remainder/internal/evidence"
)

func TestAdapter_Observe_normalizes_authoritative_limits_and_paid_usage(t *testing.T) {
	now := fixedNow()
	authFile := writeAuth(t, `{"claudeAiOauth":{"accessToken":"synthetic-access","refreshToken":"ignored","expiresAt":1788973200000}}`)
	requests := make([]string, 0, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Path)
		if r.Header.Get("Authorization") != "Bearer synthetic-access" || r.Header.Get("anthropic-beta") != oauthBeta {
			t.Errorf("headers = %#v", r.Header)
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/oauth/profile":
			_, _ = w.Write([]byte(`{"account":{"uuid":"account-1"},"organization":{"uuid":"org-1","name":"Synthetic Org"}}`))
		case "/api/oauth/usage":
			_, _ = w.Write([]byte(`{"limits":[{"group":"session","percent":0,"resets_at":"2026-09-09T20:00:00Z"},{"group":"weekly","percent":25},{"kind":"model","percent":40,"scope":{"model":{"id":"opus","display_name":"Opus"}}}],"five_hour":{"utilization":99},"extra_usage":{"is_enabled":true,"used_credits":12345,"monthly_limit":50000,"decimal_places":2}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	adapter := New(Options{AuthFile: authFile, ProfileEndpoint: server.URL + "/api/oauth/profile", UsageEndpoint: server.URL + "/api/oauth/usage", Client: server.Client(), Now: func() time.Time { return now }})
	observation, err := adapter.Observe(t.Context(), evidence.Request{Provider: "claude", Profile: "default"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(requests, ",") != "/api/oauth/profile,/api/oauth/usage" {
		t.Fatalf("request order = %v", requests)
	}
	if observation.Account.LastObserved != "account-1@org-1" || observation.Account.Binding != evidence.IdentityVerified || !observation.ObservedAt.Equal(now) {
		t.Fatalf("identity/time = %+v / %s", observation.Account, observation.ObservedAt)
	}
	if len(observation.Windows) != 4 {
		t.Fatalf("windows = %+v", observation.Windows)
	}
	assertWindow(t, observation.Windows[0], "five_hour", evidence.ScopeAccount, "percent", "0", "100")
	assertWindow(t, observation.Windows[1], "weekly", evidence.ScopeAccount, "percent", "25", "75")
	assertWindow(t, observation.Windows[2], "model_opus", evidence.ScopeModel, "percent", "40", "60")
	extra := observation.Windows[3]
	if extra.ID != "extra_usage" || extra.Unit != "credits" || len(extra.Limits) != 3 ||
		extra.Limits[0].Value.Amount == nil || extra.Limits[0].Value.Amount.String() != "123.45" ||
		extra.Limits[1].Value.Amount == nil || extra.Limits[1].Value.Amount.String() != "500" ||
		extra.Limits[2].Value.Amount == nil || extra.Limits[2].Value.Amount.String() != "376.55" {
		t.Fatalf("extra usage = %+v", extra)
	}
}

func TestAdapter_Observe_preserves_unknown_values_without_fabricating_zero(t *testing.T) {
	adapter := testAdapter(t, `{"account":{"uuid":"account-1"}}`, `{"five_hour":{"resets_at":"2026-09-09T20:00:00Z"},"extra_usage":{"is_enabled":true}}`, http.StatusOK)
	observation, err := adapter.Observe(t.Context(), evidence.Request{Provider: "claude", Profile: "default"})
	if err != nil {
		t.Fatal(err)
	}
	if len(observation.Windows) != 3 {
		t.Fatalf("windows = %+v", observation.Windows)
	}
	for _, window := range observation.Windows {
		for _, limit := range window.Limits {
			if limit.Value.State != evidence.ValueUnknown || limit.Value.Amount != nil {
				t.Fatalf("unknown limit = %+v", limit)
			}
		}
	}
}

func TestAdapter_Observe_requires_verified_profile_before_usage(t *testing.T) {
	usageCalls := 0
	authFile := writeAuth(t, `{"claudeAiOauth":{"access_token":"synthetic-access","expires_at":1788973200000}}`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/usage") {
			usageCalls++
		}
		_, _ = w.Write([]byte(`{"account":{}}`))
	}))
	defer server.Close()
	adapter := New(Options{AuthFile: authFile, ProfileEndpoint: server.URL + "/profile", UsageEndpoint: server.URL + "/usage", Client: server.Client(), Now: fixedNow})
	_, err := adapter.Observe(t.Context(), evidence.Request{Provider: "claude", Profile: "default"})
	if err == nil || usageCalls != 0 || !strings.Contains(err.Error(), "profile") {
		t.Fatalf("error = %v, usage calls = %d", err, usageCalls)
	}
}

func TestAdapter_Observe_rejects_expected_account_before_usage(t *testing.T) {
	adapter := testAdapter(t, `{"account":{"uuid":"account-1"}}`, `{"five_hour":{"utilization":1}}`, http.StatusOK)
	_, err := adapter.Observe(t.Context(), evidence.Request{Provider: "claude", Profile: "default", Account: "account-2"})
	if !errors.Is(err, evidence.ErrWrongAccount) {
		t.Fatalf("error = %v", err)
	}
}

func TestAdapter_Failure_classifies_HTTP_statuses(t *testing.T) {
	for _, tt := range []struct {
		status int
		kind   cache.FailureKind
	}{
		{http.StatusUnauthorized, cache.FailureRevoked},
		{http.StatusForbidden, cache.FailureTransient},
		{http.StatusTooManyRequests, cache.FailureTransient},
	} {
		t.Run(http.StatusText(tt.status), func(t *testing.T) {
			adapter := testAdapter(t, `{"account":{"uuid":"account-1"}}`, `{}`, tt.status)
			_, err := adapter.Observe(t.Context(), evidence.Request{Provider: "claude", Profile: "default"})
			kind, retryAt := adapter.Failure(err)
			if kind != tt.kind {
				t.Fatalf("kind = %q, want %q; error = %v", kind, tt.kind, err)
			}
			if tt.status == http.StatusTooManyRequests && retryAt.IsZero() {
				t.Fatal("Retry-After was not preserved")
			}
		})
	}
}

func TestAdapter_rejects_redirect_without_body_leak(t *testing.T) {
	secret := "synthetic-secret-that-must-not-appear"
	authFile := writeAuth(t, `{"claudeAiOauth":{"accessToken":"synthetic-access","expiresAt":1788973200000}}`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/profile" {
			http.Redirect(w, r, "/leak", http.StatusFound)
			return
		}
		_, _ = w.Write([]byte(secret))
	}))
	defer server.Close()
	adapter := New(Options{AuthFile: authFile, ProfileEndpoint: server.URL + "/profile", UsageEndpoint: server.URL + "/usage", Client: server.Client(), Now: fixedNow})
	_, err := adapter.Observe(t.Context(), evidence.Request{Provider: "claude", Profile: "default"})
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("error = %v", err)
	}
}

func TestAdapter_rejects_credential_boundary_failures(t *testing.T) {
	tests := []struct {
		name string
		make func(*testing.T) string
		want string
	}{
		{"missing", func(t *testing.T) string { return filepath.Join(t.TempDir(), "missing") }, "missing"},
		{"empty root", func(t *testing.T) string { return "" }, "directory"},
		{"malformed", func(t *testing.T) string { return writeAuth(t, "{") }, "malformed"},
		{"expired", func(t *testing.T) string {
			return writeAuth(t, `{"claudeAiOauth":{"accessToken":"synthetic-secret","expiresAt":1}}`)
		}, "expired"},
		{"oversized", func(t *testing.T) string { return writeAuth(t, strings.Repeat("x", maxAuthBytes+1)) }, "bounded regular"},
		{"symlink", func(t *testing.T) string {
			target := writeAuth(t, `{"claudeAiOauth":{"accessToken":"synthetic-secret"}}`)
			link := filepath.Join(t.TempDir(), "link")
			if err := os.Symlink(target, link); err != nil {
				t.Fatal(err)
			}
			return link
		}, "bounded regular"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapter := New(Options{AuthFile: tt.make(t), Now: fixedNow})
			_, err := adapter.Observe(t.Context(), evidence.Request{Provider: "claude", Profile: "default"})
			if err == nil || !strings.Contains(err.Error(), tt.want) || strings.Contains(err.Error(), "synthetic-secret") {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestAdapter_selection_cancellation_binding_and_account_failure(t *testing.T) {
	path := writeAuth(t, "not JSON until Observe")
	adapter := New(Options{AuthFile: path})
	binding, err := adapter.CacheBinding(t.Context(), evidence.Request{Provider: "claude", Profile: "default"})
	if err != nil || binding.Provider != "claude" || binding.SourceName != "claude_credentials_json" || binding.CredentialFingerprint == "" {
		t.Fatalf("binding = %+v, error = %v", binding, err)
	}
	_, err = adapter.Observe(t.Context(), evidence.Request{Provider: "codex", Profile: "default"})
	if !errors.Is(err, evidence.ErrInvalidSelection) {
		t.Fatalf("selection error = %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = adapter.Observe(ctx, evidence.Request{Provider: "claude", Profile: "default"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel error = %v", err)
	}
	kind, retry := adapter.Failure(evidence.ErrWrongAccount)
	if kind != cache.FailureAccountMismatch || !retry.IsZero() {
		t.Fatalf("failure = %q %s", kind, retry)
	}
}

func TestDefault_explicit_empty_config_root_does_not_fall_back_to_working_directory(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	adapter := Default()
	_, err := adapter.CacheBinding(t.Context(), evidence.Request{Provider: "claude", Profile: "default"})
	if err == nil || !strings.Contains(err.Error(), "directory could not be resolved") {
		t.Fatalf("error = %v", err)
	}
}

func assertWindow(t *testing.T, window evidence.Window, id string, scope evidence.Scope, unit, used, remaining string) {
	t.Helper()
	if window.ID != evidence.WindowID(id) || window.Scope != scope || window.Unit != unit || len(window.Limits) < 2 {
		t.Fatalf("window = %+v", window)
	}
	if window.Limits[0].Value.Amount == nil || window.Limits[0].Value.Amount.String() != used || window.Limits[1].Value.Amount == nil || window.Limits[1].Value.Amount.String() != remaining {
		t.Fatalf("limits = %+v", window.Limits)
	}
}

func testAdapter(t *testing.T, profile, usage string, usageStatus int) Adapter {
	t.Helper()
	authFile := writeAuth(t, `{"claudeAiOauth":{"accessToken":"synthetic-access","expiresAt":1788973200000}}`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/profile") {
			_, _ = w.Write([]byte(profile))
			return
		}
		if usageStatus == http.StatusTooManyRequests {
			w.Header().Set("Retry-After", "30")
		}
		w.WriteHeader(usageStatus)
		_, _ = w.Write([]byte(usage))
	}))
	t.Cleanup(server.Close)
	return New(Options{AuthFile: authFile, ProfileEndpoint: server.URL + "/profile", UsageEndpoint: server.URL + "/usage", Client: server.Client(), Now: fixedNow})
}

func writeAuth(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), ".credentials.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func fixedNow() time.Time { return time.Date(2026, time.September, 9, 16, 0, 0, 0, time.UTC) }
