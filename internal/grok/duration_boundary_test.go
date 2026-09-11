package grok

import (
	"net/http"
	"testing"
	"time"
)

func TestPeriodDurationBoundary(t *testing.T) {
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	used := float32(25)
	for _, test := range []struct {
		name         string
		start, reset time.Time
		wantError    bool
	}{
		{"representable", now.Add(-24 * time.Hour), now.Add(9 * 24 * time.Hour), false},
		{"saturated", time.Unix(0, 0).UTC(), time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC), true},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := authPath(t, "grok.com", "synthetic-duration", "", "", "")
			adapter, _ := testAdapter(t, func(w http.ResponseWriter, _ *http.Request) {
				writeQuota(w, quotaPayload(&used, nil, 2, test.start, test.reset, nil))
			}, path, now)
			observation, err := adapter.Observe(t.Context(), request())
			if test.wantError {
				if err == nil {
					t.Fatal("accepted unrepresentable cycle duration")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			duration := limit(t, observation, "credits", "credits_duration").Duration
			reset := limit(t, observation, "credits", "credits_reset").ResetAt
			if duration == nil || *duration != 240*time.Hour || reset == nil || !reset.Equal(test.reset) {
				t.Fatalf("cycle changed: duration=%v reset=%v", duration, reset)
			}
		})
	}
}
