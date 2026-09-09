package cursor

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/douglasjarquin/remainder/internal/evidence"
)

func TestAdapterObserve_rejectsQuotedNonJSONNumbers(t *testing.T) {
	for _, raw := range []string{`"0x1p2"`, `"1_000"`, `".5"`} {
		t.Run(raw, func(t *testing.T) {
			// Given
			server := fixtureServer(t, map[string]fixtureResponse{
				"GetCurrentPeriodUsage": {body: fmt.Sprintf(`{"planUsage":{"totalPercentUsed":%s}}`, raw)},
				"GetPlanInfo":           {body: `{}`},
				"GetSandUsageStatus":    {body: `{}`},
			})
			defer server.Close()

			// When
			_, err := testAdapter(t, server).Observe(t.Context(), cursorRequest())

			// Then
			if err == nil {
				t.Fatalf("Observe() accepted non-JSON number %s", raw)
			}
		})
	}
}

func TestAdapterObserve_preservesPaidAmountPrecision(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{name: "large integer", raw: "9007199254740993"},
		{name: "fractional exponent", raw: "1.234567890123456789e2"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Given
			server := fixtureServer(t, map[string]fixtureResponse{
				"GetCurrentPeriodUsage": {body: fmt.Sprintf(`{"spendLimitUsage":{"individualLimit":%q}}`, test.raw)},
				"GetPlanInfo":           {body: `{}`},
				"GetSandUsageStatus":    {body: `{}`},
			})
			defer server.Close()

			// When
			observation, err := testAdapter(t, server).Observe(t.Context(), cursorRequest())
			// Then
			if err != nil {
				t.Fatal(err)
			}
			assertValue(t, observation, "spend_limit", evidence.Field("limit"), test.raw+"\n")
		})
	}
}

func TestAdapterObserve_rejectsQuotedNonJSONTimestamps(t *testing.T) {
	for _, raw := range []string{"0x1p2", "1_000", ".5"} {
		t.Run(raw, func(t *testing.T) {
			// Given
			server := fixtureServer(t, map[string]fixtureResponse{
				"GetCurrentPeriodUsage": {body: fmt.Sprintf(`{"billingCycleStart":%q,"planUsage":{"totalPercentUsed":25}}`, raw)},
				"GetPlanInfo":           {body: `{}`},
				"GetSandUsageStatus":    {body: `{}`},
			})
			defer server.Close()

			// When
			_, err := testAdapter(t, server).Observe(t.Context(), cursorRequest())
			// Then
			if err == nil {
				t.Fatalf("Observe() accepted non-JSON timestamp %q", raw)
			}
		})
	}
}

func TestAdapterObserve_omitsUnrepresentableCycleDuration(t *testing.T) {
	// Given
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch rpcPath := r.URL.Path; rpcPath {
		case "/aiserver.v1.DashboardService/GetCurrentPeriodUsage":
			fmt.Fprint(w, `{"billingCycleStart":"1970-01-01T00:00:00Z","billingCycleEnd":"9999-01-01T00:00:00Z","planUsage":{"totalPercentUsed":25}}`)
		default:
			fmt.Fprint(w, `{}`)
		}
	}))
	defer server.Close()

	// When
	observation, err := testAdapter(t, server).Observe(t.Context(), cursorRequest())
	// Then
	if err != nil {
		t.Fatal(err)
	}
	window := windowByID(t, observation, "included_usage")
	for _, limit := range window.Limits {
		if limit.Field == evidence.FieldDuration {
			t.Fatalf("duration = %v, want unknown because the source span is not representable", limit.Duration)
		}
	}
	if reset := limitByField(t, window, evidence.FieldReset).ResetAt; reset == nil || reset.Year() != 9999 {
		t.Fatalf("reset = %v, want source reset retained", reset)
	}
}
