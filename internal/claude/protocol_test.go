package claude

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/douglasjarquin/remainder/internal/evidence"
)

func TestAdapter_rejects_oversized_and_malformed_responses_without_body_leak(t *testing.T) {
	secret := "synthetic-secret-that-must-not-appear"
	for _, tt := range []struct{ name, body, want string }{
		{"oversized", strings.Repeat("x", maxResponseBytes+1), "too large"},
		{"malformed", `{"secret":"synthetic-secret-that-must-not-appear"`, "malformed JSON"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			authFile := writeAuth(t, `{"claudeAiOauth":{"accessToken":"synthetic-access","expiresAt":1788973200000}}`)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/profile") {
					_, _ = w.Write([]byte(`{"account":{"uuid":"account-1"}}`))
					return
				}
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()
			adapter := New(Options{AuthFile: authFile, ProfileEndpoint: server.URL + "/profile", UsageEndpoint: server.URL + "/usage", Client: server.Client(), Now: fixedNow})
			_, err := adapter.Observe(t.Context(), evidence.Request{Provider: "claude", Profile: "default"})
			if err == nil || !strings.Contains(err.Error(), tt.want) || strings.Contains(err.Error(), secret) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestAdapter_timeout_cancels_shared_profile_usage_budget(t *testing.T) {
	authFile := writeAuth(t, `{"claudeAiOauth":{"accessToken":"synthetic-access","expiresAt":1788973200000}}`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/profile") {
			_, _ = w.Write([]byte(`{"account":{"uuid":"account-1"}}`))
			return
		}
		<-r.Context().Done()
	}))
	defer server.Close()
	adapter := New(Options{AuthFile: authFile, ProfileEndpoint: server.URL + "/profile", UsageEndpoint: server.URL + "/usage", Client: server.Client(), Timeout: 20 * time.Millisecond, Now: fixedNow})
	_, err := adapter.Observe(t.Context(), evidence.Request{Provider: "claude", Profile: "default"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v", err)
	}
}

func TestNormalize_scoped_model_groups_and_unscaled_paid_values_remain_distinct(t *testing.T) {
	adapter := testAdapter(t, `{"account":{"uuid":"account-1"}}`, `{"limits":[{"group":"session","percent":1,"scope":{"model":{"id":"opus","display_name":"Opus"}}},{"group":"weekly","percent":2,"scope":{"model":{"id":"opus","display_name":"Opus"}}}],"extra_usage":{"is_enabled":true,"used_credits":12345,"monthly_limit":50000}}`, http.StatusOK)
	observation, err := adapter.Observe(t.Context(), evidence.Request{Provider: "claude", Profile: "default"})
	if err != nil {
		t.Fatal(err)
	}
	if len(observation.Windows) != 3 || observation.Windows[0].ID != "model_opus_session" || observation.Windows[1].ID != "model_opus_weekly" || observation.Windows[2].Unit != "credits_native" {
		t.Fatalf("windows = %+v", observation.Windows)
	}
	if got := observation.Windows[2].Limits[0].Value.Amount; got == nil || got.String() != "12345" {
		t.Fatalf("native used = %+v", got)
	}
}

func TestNormalize_present_empty_authoritative_limits_fails_closed(t *testing.T) {
	adapter := testAdapter(t, `{"account":{"uuid":"account-1"}}`, `{"limits":[],"five_hour":{"utilization":10}}`, http.StatusOK)
	_, err := adapter.Observe(t.Context(), evidence.Request{Provider: "claude", Profile: "default"})
	if err == nil || !strings.Contains(err.Error(), "no limits") {
		t.Fatalf("error = %v", err)
	}
}
