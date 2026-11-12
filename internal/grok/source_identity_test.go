package grok

import (
	"net/http"
	"testing"
	"time"
)

func TestSourceIdentity_cacheBindingMatchesObservation(t *testing.T) {
	now := time.Date(2026, time.September, 9, 12, 0, 0, 0, time.UTC)
	shared := float32(25)
	adapter, _ := testAdapter(t, func(w http.ResponseWriter, _ *http.Request) {
		writeQuota(w, quotaPayload(&shared, nil, 2, now.Add(-24*time.Hour), now.Add(24*time.Hour), nil))
	}, authPath(t, "grok.com", "synthetic-token", "", "", ""), now)

	binding, bindingErr := adapter.CacheBinding(t.Context(), request())
	observation, observeErr := adapter.Observe(t.Context(), request())

	if bindingErr != nil || observeErr != nil || binding.SourceKind != observation.Source.Kind || binding.SourceName != observation.Source.Name {
		t.Fatalf("binding = %+v (%v), source = %+v (%v)", binding, bindingErr, observation.Source, observeErr)
	}
}
