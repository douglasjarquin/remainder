package codex

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/douglasjarquin/remainder/internal/evidence"
)

func TestAdapterObserve_assignsUnknownCounterpartAfterPresentWindowIdentity(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		known   evidence.WindowID
		missing evidence.WindowID
	}{
		{name: "weekly primary", body: `{"account_id":"acct-test","rate_limit":{"primary_window":{"used_percent":20,"limit_window_seconds":604800},"secondary_window":null}}`, known: "weekly", missing: "five_hour"},
		{name: "five hour secondary", body: `{"account_id":"acct-test","rate_limit":{"primary_window":null,"secondary_window":{"used_percent":20,"limit_window_seconds":18000}}}`, known: "five_hour", missing: "weekly"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, test.body) }))
			defer server.Close()
			observation, err := newTestAdapter(t, server.Client(), []string{server.URL}).Observe(t.Context(), evidence.Request{Provider: "codex", Profile: "default"})
			if err != nil {
				t.Fatalf("Observe() error = %v", err)
			}
			remaining, err := evidence.SelectValue(observation, evidence.ValueRequest{Provider: "codex", Profile: "default", Window: test.known, Field: evidence.FieldRemaining}, evidence.FreshOnly)
			if err != nil || remaining != "80\n" {
				t.Fatalf("known remaining = %q, %v, want 80", remaining, err)
			}
			if _, err := evidence.SelectValue(observation, evidence.ValueRequest{Provider: "codex", Profile: "default", Window: test.missing, Field: evidence.FieldRemaining}, evidence.FreshOnly); !errors.Is(err, evidence.ErrUndefined) {
				t.Fatalf("missing counterpart error = %v, want undefined", err)
			}
		})
	}
}

func TestDefaultEndpointUsesCurrentWhamRouteOnly(t *testing.T) {
	if len(defaultEndpoints) != 1 || defaultEndpoints[0] != "https://chatgpt.com/backend-api/wham/usage" {
		t.Fatalf("defaultEndpoints = %v, want only current wham usage route", defaultEndpoints)
	}
}

func TestAdapterObserve_rejectsGenuinelyCollidingSuppliedWindows(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"account_id":"acct-test","rate_limit":{"primary_window":{"used_percent":20,"limit_window_seconds":604800},"secondary_window":{"used_percent":30,"limit_window_seconds":604800}}}`)
	}))
	defer server.Close()
	_, err := newTestAdapter(t, server.Client(), []string{server.URL}).Observe(t.Context(), evidence.Request{Provider: "codex", Profile: "default"})
	if err == nil || !errors.Is(err, evidence.ErrDuplicateID) {
		t.Fatalf("Observe() error = %v, want duplicate supplied window rejection", err)
	}
}
