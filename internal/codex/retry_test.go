package codex

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/douglasjarquin/remainder/internal/evidence"
)

func TestAdapterObserve_RetryAfter_returnsTypedDeadline(t *testing.T) {
	// Given
	now := time.Date(2026, time.September, 9, 12, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()
	adapter := New(Options{AuthFile: writeTestAuth(t), Endpoints: []string{server.URL}, Client: server.Client(), Timeout: time.Second, Now: func() time.Time { return now }})

	// When
	_, err := adapter.Observe(t.Context(), evidence.Request{Provider: "codex", Profile: "default"})

	// Then
	retry, ok := errors.AsType[*RetryError](err)
	if !ok || !errors.Is(err, ErrTransient) || !retry.RetryAt.Equal(now.Add(30*time.Second)) {
		t.Fatalf("error = %v, retry = %+v", err, retry)
	}
}
