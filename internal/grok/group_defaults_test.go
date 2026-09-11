package grok

import (
	"math"
	"net/http"
	"testing"
	"time"

	"github.com/douglasjarquin/remainder/internal/evidence"
)

func TestAdapterObserve_rejectsKnownGroupWireTypes(t *testing.T) {
	// Given
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	start := now.Add(-time.Hour)
	reset := now.Add(time.Hour)

	for _, test := range []struct {
		name        string
		configField []byte
		wantErr     bool
	}{
		{"shared_usage", emptyGroupField(1), true},
		{"product_kind", bytesField(7, emptyGroupField(1)), true},
		{"product_usage", bytesField(7, append(varintField(1, 4), emptyGroupField(2)...)), true},
		{"prepaid_balance", bytesField(12, emptyGroupField(1)), true},
		{"shared_group_and_scalar", append(emptyGroupField(1), fixed32Field(1, math.Float32bits(25))...), true},
		{"unknown_group", append(emptyGroupField(31), fixed32Field(1, math.Float32bits(25))...), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := authPath(t, "grok.com", "group-wire-session", "", "", "")
			adapter, _ := testAdapter(t, func(w http.ResponseWriter, _ *http.Request) {
				writeQuota(w, quotaPayloadWithConfigField(t, start, reset, test.configField))
			}, path, now)

			// When
			observation, err := adapter.Observe(t.Context(), request())

			// Then
			if test.wantErr {
				if err == nil {
					t.Fatalf("group wire type was accepted: %+v", observation)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := limit(t, observation, "credits", "credits_used").Value; got.State != evidence.ValueDefined || got.Amount == nil || got.Amount.String() != "25" {
				t.Fatalf("unknown group changed shared usage: %+v", got)
			}
		})
	}
}

func quotaPayloadWithConfigField(t *testing.T, start, reset time.Time, field []byte) []byte {
	t.Helper()
	response := mustScan(t, quotaPayload(nil, nil, 2, start, reset, nil))
	config, present, err := bytesAt(response, 1)
	if err != nil || !present {
		t.Fatalf("credits config = %x, present = %t, error = %v", config, present, err)
	}
	config = append(config, field...)
	return bytesField(1, config)
}

func emptyGroupField(number uint64) []byte {
	field := varint(number<<3 | 3)
	return append(field, varint(number<<3|4)...)
}
