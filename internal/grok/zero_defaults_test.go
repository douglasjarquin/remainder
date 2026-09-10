package grok

import (
	"math"
	"net/http"
	"testing"
	"time"

	"github.com/douglasjarquin/remainder/internal/evidence"
)

func TestAdapterObserve_defaultsOmittedProtoScalarsWhenCycleIsValid(t *testing.T) {
	// Given
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	start := now.Add(-7 * 24 * time.Hour)
	reset := now.Add(24 * time.Hour)
	path := authPath(t, "grok.com", "omitted-zero-session", "", "", "")
	adapter, _ := testAdapter(t, func(w http.ResponseWriter, _ *http.Request) {
		writeQuota(w, nativeOmittedScalarPayload(start, reset))
	}, path, now)

	// When
	observation, err := adapter.Observe(t.Context(), request())

	// Then
	if err != nil {
		t.Fatal(err)
	}
	if observation.Outcome != evidence.OutcomeComplete {
		t.Fatalf("outcome = %s", observation.Outcome)
	}
	for _, check := range []struct {
		window string
		limit  string
		state  evidence.ValueState
		amount string
	}{
		{"credits", "credits_used", evidence.ValueZero, "0"},
		{"credits", "credits_remaining", evidence.ValueDefined, "100"},
		{"prepaid", "prepaid_remaining", evidence.ValueZero, "0"},
	} {
		if got := limit(t, observation, check.window, check.limit).Value; got.State != check.state || got.Amount == nil || got.Amount.String() != check.amount {
			t.Fatalf("%s/%s = %+v", check.window, check.limit, got)
		}
	}
	t.Logf("native omitted-scalar observation=%+v", observation)
}

func TestAdapterObserve_defaultsOmittedProductKindAndUsageWhenCycleIsValid(t *testing.T) {
	// Given
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	start := now.Add(-7 * 24 * time.Hour)
	reset := now.Add(24 * time.Hour)
	path := authPath(t, "grok.com", "omitted-product-session", "", "", "")
	adapter, _ := testAdapter(t, func(w http.ResponseWriter, _ *http.Request) {
		writeQuota(w, omittedProductPayload(start, reset))
	}, path, now)

	// When
	observation, err := adapter.Observe(t.Context(), request())

	// Then
	if err != nil {
		t.Fatal(err)
	}
	for _, check := range []struct {
		limit  string
		state  evidence.ValueState
		amount string
	}{
		{"product:unspecified_used", evidence.ValueZero, "0"},
		{"product:unspecified_remaining", evidence.ValueDefined, "100"},
	} {
		if got := limit(t, observation, "product:unspecified", check.limit).Value; got.State != check.state || got.Amount == nil || got.Amount.String() != check.amount {
			t.Fatalf("product:unspecified/%s = %+v", check.limit, got)
		}
	}
}

func TestAdapterObserve_preservesExplicitScalarValues(t *testing.T) {
	// Given
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	start := now.Add(-7 * 24 * time.Hour)
	reset := now.Add(24 * time.Hour)
	shared, used := float32(25), float32(40)
	prepaid := uint64(120)
	path := authPath(t, "grok.com", "explicit-scalar-session", "", "", "")
	adapter, _ := testAdapter(t, func(w http.ResponseWriter, _ *http.Request) {
		writeQuota(w, quotaPayload(&shared, []productFixture{{kind: 4, used: &used}}, 2, start, reset, &prepaid))
	}, path, now)

	// When
	observation, err := adapter.Observe(t.Context(), request())

	// Then
	if err != nil {
		t.Fatal(err)
	}
	for _, check := range []struct {
		window string
		limit  string
		amount string
	}{
		{"credits", "credits_used", "25"},
		{"product:chat", "product:chat_used", "40"},
		{"prepaid", "prepaid_remaining", "120"},
	} {
		if got := limit(t, observation, check.window, check.limit).Value; got.State != evidence.ValueDefined || got.Amount == nil || got.Amount.String() != check.amount {
			t.Fatalf("%s/%s = %+v", check.window, check.limit, got)
		}
	}
}

func TestAdapterObserve_keepsMissingUsageUnknownWithoutValidCycle(t *testing.T) {
	// Given
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	path := authPath(t, "grok.com", "missing-usage-session", "", "", "")
	adapter, _ := testAdapter(t, func(w http.ResponseWriter, _ *http.Request) {
		writeQuota(w, quotaPayload(nil, []productFixture{{kind: 4}}, 0, time.Time{}, time.Time{}, nil))
	}, path, now)

	// When
	observation, err := adapter.Observe(t.Context(), request())

	// Then
	if err != nil {
		t.Fatal(err)
	}
	if observation.Outcome != evidence.OutcomePartial || limit(t, observation, "product:chat", "product:chat_used").Value.State != evidence.ValueUnknown {
		t.Fatalf("observation = %+v", observation)
	}
}

func TestAdapterObserve_rejectsInvalidScalarEncodings(t *testing.T) {
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	start := now.Add(-7 * 24 * time.Hour)
	reset := now.Add(24 * time.Hour)

	for _, test := range []struct {
		name    string
		payload []byte
	}{
		{"missing_config", nil},
		{"missing_entitlements", bytesField(1, nil)},
		{"malformed", []byte{0x80}},
		{"wrong_wire", bytesField(1, append(varintField(1, 0), cyclePayload(start, reset)...))},
		{"duplicate", bytesField(1, append(append(fixed32Field(1, math.Float32bits(1)), fixed32Field(1, math.Float32bits(2))...), cyclePayload(start, reset)...))},
		{"out_of_range", bytesField(1, append(fixed32Field(1, math.Float32bits(101)), cyclePayload(start, reset)...))},
	} {
		t.Run(test.name, func(t *testing.T) {
			// Given
			path := authPath(t, "grok.com", "invalid-scalar-session", "", "", "")
			adapter, _ := testAdapter(t, func(w http.ResponseWriter, _ *http.Request) {
				writeQuota(w, test.payload)
			}, path, now)

			// When
			_, err := adapter.Observe(t.Context(), request())

			// Then
			if err == nil {
				t.Fatal("invalid scalar payload was accepted")
			}
		})
	}
}

func nativeOmittedScalarPayload(start, reset time.Time) []byte {
	config := cyclePayload(start, reset)
	config = append(config, bytesField(12, nil)...)
	return bytesField(1, config)
}

func omittedProductPayload(start, reset time.Time) []byte {
	config := cyclePayload(start, reset)
	config = append(config, bytesField(7, nil)...)
	return bytesField(1, config)
}

func cyclePayload(start, reset time.Time) []byte {
	cycle := varintField(1, 2)
	cycle = append(cycle, bytesField(2, timestampMessage(start))...)
	cycle = append(cycle, bytesField(3, timestampMessage(reset))...)
	return bytesField(8, cycle)
}
