package grok

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/douglasjarquin/remainder/internal/evidence"
)

func TestAdapterObserve(t *testing.T) {
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	start := now.Add(-7 * 24 * time.Hour)
	reset := now.Add(24 * time.Hour)
	shared, chat := float32(25), float32(40)
	prepaid := uint64(120)

	t.Run("valid_shared_products_prepaid", func(t *testing.T) {
		path := authPath(t, "https://accounts.x.ai/sign-in", "consumer-session", "local@example.test", "team-7", now.Add(time.Hour).Format(time.RFC3339))
		adapter, _ := testAdapter(t, func(w http.ResponseWriter, r *http.Request) {
			assertSourceRequest(t, r, "consumer-session")
			writeQuota(w, quotaPayload(&shared, []productFixture{{kind: 4, used: &chat}}, 2, start, reset, &prepaid))
		}, path, now)

		observation, err := adapter.Observe(t.Context(), request())
		if err != nil {
			t.Fatal(err)
		}
		if err := observation.Validate(); err != nil {
			t.Fatalf("invalid observation: %v", err)
		}
		if len(observation.Windows) != 3 || observation.Account.LastObserved != "" || observation.Account.Binding != evidence.IdentityUnknown {
			t.Fatalf("observation = %+v", observation)
		}
		if got := limit(t, observation, "credits", "credits_remaining").Value.Amount; got == nil || got.String() != "75" {
			t.Fatalf("shared remaining = %v", got)
		}
		if got := limit(t, observation, "product:chat", "product:chat_used").Value.Amount; got == nil || got.String() != "40" {
			t.Fatalf("chat used = %v", got)
		}
		if got := limit(t, observation, "prepaid", "prepaid_remaining").Value.Amount; got == nil || got.String() != "120" {
			t.Fatalf("prepaid = %v", got)
		}
	})

	t.Run("zero_values", func(t *testing.T) {
		zero := float32(0)
		balance := uint64(0)
		path := authPath(t, "grok.com", "zero-session", "", "", "")
		adapter, _ := testAdapter(t, func(w http.ResponseWriter, _ *http.Request) {
			writeQuota(w, quotaPayload(&zero, nil, 2, start, reset, &balance))
		}, path, now)
		observation, err := adapter.Observe(t.Context(), request())
		if err != nil {
			t.Fatal(err)
		}
		if limit(t, observation, "credits", "credits_used").Value.State != evidence.ValueZero || limit(t, observation, "prepaid", "prepaid_remaining").Value.State != evidence.ValueZero {
			t.Fatalf("zero values were not preserved: %+v", observation.Windows)
		}
	})

	t.Run("unknown_fields", func(t *testing.T) {
		path := authPath(t, "https://grok.com", "unknown-session", "", "", "")
		payload := quotaPayload(nil, []productFixture{{kind: 4}}, 2, start, reset, nil)
		payload = append(payload, varintField(31, 99)...)
		payload = append(payload, varint(32<<3|3)...)
		payload = append(payload, varintField(1, 1)...)
		payload = append(payload, varint(32<<3|4)...)
		adapter, _ := testAdapter(t, func(w http.ResponseWriter, _ *http.Request) { writeQuota(w, payload) }, path, now)
		observation, err := adapter.Observe(t.Context(), request())
		if err != nil {
			t.Fatal(err)
		}
		if observation.Outcome != evidence.OutcomeComplete || limit(t, observation, "credits", "credits_used").Value.State != evidence.ValueZero || limit(t, observation, "product:chat", "product:chat_remaining").Value.State != evidence.ValueDefined {
			t.Fatalf("defaults = %+v", observation)
		}
	})

	t.Run("changed_cycle", func(t *testing.T) {
		path := authPath(t, "grok.com", "cycle-session", "", "", "")
		for _, cycle := range []struct {
			name   string
			period uint64
			start  time.Time
		}{{"weekly", 2, reset.Add(-7 * 24 * time.Hour)}, {"monthly", 1, reset.Add(-30 * 24 * time.Hour)}} {
			t.Run(cycle.name, func(t *testing.T) {
				adapter, _ := testAdapter(t, func(w http.ResponseWriter, _ *http.Request) {
					writeQuota(w, quotaPayload(&shared, nil, cycle.period, cycle.start, reset, nil))
				}, path, now)
				observation, err := adapter.Observe(t.Context(), request())
				if err != nil {
					t.Fatal(err)
				}
				duration := limit(t, observation, "credits", "credits_duration").Duration
				if duration == nil || *duration != reset.Sub(cycle.start) || observation.Windows[0].ID != "credits" {
					t.Fatalf("cycle = %+v", observation.Windows[0])
				}
			})
		}
	})

	t.Run("default_precedence", func(t *testing.T) {
		first := authPath(t, "grok.com", "first-session", "", "", "")
		second := authPath(t, "grok.com", "second-session", "", "", "")
		t.Setenv("GROK_AUTH_JSON", first)
		t.Setenv("GROK_AUTH_PATH", second)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assertSourceRequest(t, r, "first-session")
			writeQuota(w, quotaPayload(&shared, nil, 2, start, reset, nil))
		}))
		defer server.Close()
		_, err := New(Options{Client: server.Client(), Endpoint: server.URL, Now: func() time.Time { return now }}).Observe(t.Context(), request())
		if err != nil {
			t.Fatal(err)
		}
	})

	credentialErrors := []struct {
		name string
		file func(*testing.T) string
	}{{"wrong_scope", func(t *testing.T) string { return authPath(t, "example.com", "wrong-scope-secret", "", "", "") }}, {"expired", func(t *testing.T) string {
		return authPath(t, "grok.com", "expired-secret", "", "", now.Add(-time.Second).Format(time.RFC3339))
	}}, {"api_key_rejected", func(t *testing.T) string { return authPath(t, "api.x.ai", "api-key-secret", "", "", "") }}, {"multiple_accounts", multipleAccountsPath}}
	for _, tc := range credentialErrors {
		t.Run(tc.name, func(t *testing.T) {
			var requests atomic.Int32
			adapter, _ := testAdapter(t, func(http.ResponseWriter, *http.Request) { requests.Add(1) }, tc.file(t), now)
			_, err := adapter.Observe(t.Context(), request())
			if err == nil || requests.Load() != 0 || strings.Contains(err.Error(), "secret") {
				t.Fatalf("error = %v, requests = %d", err, requests.Load())
			}
		})
	}

	t.Run("explicit_empty", func(t *testing.T) {
		t.Setenv("GROK_AUTH_JSON", "")
		t.Setenv("GROK_AUTH_PATH", authPath(t, "grok.com", "fallback-secret", "", "", ""))
		_, err := Default().Observe(t.Context(), request())
		if err == nil || strings.Contains(err.Error(), "fallback-secret") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("status_401", statusCase(now, http.StatusUnauthorized, ErrAuthorizationRejected))
	t.Run("status_403", statusCase(now, http.StatusForbidden, ErrAuthorizationRejected))
	t.Run("status_429", statusCase(now, http.StatusTooManyRequests, ErrTransient))
	t.Run("redirect_refused", func(t *testing.T) {
		var followed atomic.Int32
		target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { followed.Add(1) }))
		defer target.Close()
		path := authPath(t, "grok.com", "redirect-secret", "", "", "")
		adapter, _ := testAdapter(t, func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) }, path, now)
		_, err := adapter.Observe(t.Context(), request())
		if err == nil || followed.Load() != 0 {
			t.Fatalf("error = %v, followed = %d", err, followed.Load())
		}
	})

	t.Run("oversize", func(t *testing.T) {
		path := authPath(t, "grok.com", "oversize-secret", "", "", "")
		adapter, _ := testAdapter(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(make([]byte, maxResponseBytes+1)) }, path, now)
		_, err := adapter.Observe(t.Context(), request())
		if err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("malformed_protobuf", func(t *testing.T) {
		path := authPath(t, "grok.com", "protobuf-secret", "", "", "")
		adapter, _ := testAdapter(t, func(w http.ResponseWriter, _ *http.Request) { writeQuota(w, []byte{0x80}) }, path, now)
		_, err := adapter.Observe(t.Context(), request())
		if err == nil || strings.Contains(err.Error(), "protobuf-secret") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("timeout", func(t *testing.T) {
		path := authPath(t, "grok.com", "timeout-secret", "", "", "")
		release := make(chan struct{})
		server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) { <-release }))
		defer server.Close()
		defer close(release)
		adapter := New(Options{AuthFile: path, Client: server.Client(), Endpoint: server.URL, Timeout: 10 * time.Millisecond, Now: func() time.Time { return now }})
		_, err := adapter.Observe(t.Context(), request())
		if !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, ErrTransient) {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("canceled", func(t *testing.T) {
		path := authPath(t, "grok.com", "cancel-secret", "", "", "")
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		_, err := New(Options{AuthFile: path}).Observe(ctx, request())
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("retry", func(t *testing.T) {
		path := authPath(t, "grok.com", "retry-secret", "", "", "")
		adapter, _ := testAdapter(t, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Retry-After", "30")
			w.WriteHeader(http.StatusTooManyRequests)
		}, path, now)
		_, err := adapter.Observe(t.Context(), request())
		retry, ok := errors.AsType[*RetryError](err)
		if !ok || !retry.RetryAt.Equal(now.Add(30*time.Second)) {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("errors_do_not_include_secrets", func(t *testing.T) {
		path := authPath(t, "grok.com", "never-print-this-secret", "", "", "")
		adapter := New(Options{AuthFile: path, Client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("never-print-this-secret") })}})
		_, err := adapter.Observe(t.Context(), request())
		if err == nil || strings.Contains(err.Error(), "never-print-this-secret") {
			t.Fatalf("unsafe error = %v", err)
		}
	})
}

func multipleAccountsPath(t *testing.T) string {
	t.Helper()
	path := authPath(t, "grok.com", "first", "first@example.test", "", "")
	body := `{"grok.com":{"key":"first","email":"first@example.test"},"www.grok.com":{"key":"second","email":"second@example.test"}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func statusCase(now time.Time, status int, target error) func(*testing.T) {
	return func(t *testing.T) {
		path := authPath(t, "grok.com", "status-secret", "", "", "")
		adapter, _ := testAdapter(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status) }, path, now)
		_, err := adapter.Observe(t.Context(), request())
		if !errors.Is(err, target) || strings.Contains(err.Error(), "status-secret") {
			t.Fatalf("error = %v", err)
		}
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }
