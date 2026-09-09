package claude

import (
	"net/http"
	"testing"

	"github.com/douglasjarquin/remainder/internal/evidence"
)

func TestAdapter_Observe_preserves_exact_paid_numbers(t *testing.T) {
	for _, tt := range []struct {
		name      string
		usage     string
		unit      string
		wantUsed  string
		wantLimit string
		wantLeft  string
		leftState evidence.ValueState
	}{
		{
			name:      "fraction and exponent with explicit scale",
			usage:     `{"extra_usage":{"is_enabled":true,"used_credits":250.5,"monthly_limit":1e3,"decimal_places":2}}`,
			unit:      "credits",
			wantUsed:  "2.505",
			wantLimit: "10",
			wantLeft:  "7.495",
			leftState: evidence.ValueDefined,
		},
		{
			name:      "large precise fraction",
			usage:     `{"extra_usage":{"is_enabled":true,"used_credits":900719925474099312345678.9,"monthly_limit":900719925474099312346000.0,"decimal_places":3}}`,
			unit:      "credits",
			wantUsed:  "900719925474099312345.6789",
			wantLimit: "900719925474099312346",
			wantLeft:  "0.3211",
			leftState: evidence.ValueDefined,
		},
		{
			name:      "exponent zero without scale",
			usage:     `{"extra_usage":{"is_enabled":true,"used_credits":0e3,"monthly_limit":2e3}}`,
			unit:      "credits_native",
			wantUsed:  "0",
			wantLimit: "2000",
			wantLeft:  "2000",
			leftState: evidence.ValueDefined,
		},
		{
			name:      "over limit keeps known values",
			usage:     `{"extra_usage":{"is_enabled":true,"used_credits":1000.1,"monthly_limit":1e3,"decimal_places":2}}`,
			unit:      "credits",
			wantUsed:  "10.001",
			wantLimit: "10",
			leftState: evidence.ValueUnknown,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			adapter := testAdapter(t, `{"account":{"uuid":"account-1"}}`, tt.usage, http.StatusOK)
			observation, err := adapter.Observe(t.Context(), evidence.Request{Provider: "claude", Profile: "default"})
			if err != nil {
				t.Fatal(err)
			}
			if len(observation.Windows) != 3 {
				t.Fatalf("windows = %+v", observation.Windows)
			}
			window := observation.Windows[2]
			if window.ID != "extra_usage" || window.Unit != tt.unit {
				t.Fatalf("window = %+v", window)
			}
			assertPaidValue(t, window.Limits[0].Value, tt.wantUsed)
			assertPaidValue(t, window.Limits[1].Value, tt.wantLimit)
			if window.Limits[2].Value.State != tt.leftState {
				t.Fatalf("remaining state = %q, want %q", window.Limits[2].Value.State, tt.leftState)
			}
			if tt.leftState != evidence.ValueUnknown {
				assertPaidValue(t, window.Limits[2].Value, tt.wantLeft)
			}
		})
	}
}

func assertPaidValue(t *testing.T, value evidence.Value, want string) {
	t.Helper()
	if value.Amount == nil || value.Amount.String() != want {
		t.Fatalf("value = %+v, want amount %s", value, want)
	}
	if want == "0" && value.State != evidence.ValueZero {
		t.Fatalf("zero state = %q, want %q", value.State, evidence.ValueZero)
	}
}

func TestAdapter_Observe_rejects_invalid_paid_numbers(t *testing.T) {
	for _, tt := range []struct {
		name  string
		usage string
	}{
		{"negative", `{"extra_usage":{"is_enabled":true,"used_credits":-0.1,"decimal_places":2}}`},
		{"nonfinite", `{"extra_usage":{"is_enabled":true,"used_credits":"NaN","decimal_places":2}}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			adapter := testAdapter(t, `{"account":{"uuid":"account-1"}}`, tt.usage, http.StatusOK)
			if _, err := adapter.Observe(t.Context(), evidence.Request{Provider: "claude", Profile: "default"}); err == nil {
				t.Fatal("Observe() error = nil")
			}
		})
	}
}
