package devin

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSourceIdentity_cacheBindingMatchesObservation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(fullPayload()))
	}))
	defer server.Close()
	adapter := testAdapter(t, writeCredentials(t, `windsurf_api_key = "synthetic-devin-key"`+"\n"), server)

	binding, bindingErr := adapter.CacheBinding(t.Context(), testRequest())
	observation, observeErr := adapter.Observe(t.Context(), testRequest())

	if bindingErr != nil || observeErr != nil || binding.SourceKind != observation.Source.Kind || binding.SourceName != observation.Source.Name {
		t.Fatalf("binding = %+v (%v), source = %+v (%v)", binding, bindingErr, observation.Source, observeErr)
	}
}
