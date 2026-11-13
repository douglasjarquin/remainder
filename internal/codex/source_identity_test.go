package codex

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/douglasjarquin/remainder/internal/evidence"
)

func TestSourceIdentity_cacheBindingMatchesObservation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"account_id":"acct-test","rate_limit":{"primary_window":{"used_percent":1}}}`))
	}))
	defer server.Close()
	adapter := newTestAdapter(t, server.Client(), []string{server.URL})
	request := evidence.Request{Provider: "codex", Profile: "default"}

	binding, bindingErr := adapter.CacheBinding(t.Context(), request)
	observation, observeErr := adapter.Observe(t.Context(), request)

	if bindingErr != nil || observeErr != nil || binding.SourceKind != observation.Source.Kind || binding.SourceName != observation.Source.Name {
		t.Fatalf("binding = %+v (%v), source = %+v (%v)", binding, bindingErr, observation.Source, observeErr)
	}
}
