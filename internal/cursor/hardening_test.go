package cursor

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/douglasjarquin/remainder/internal/evidence"
)

func TestAdapterValidation_prioritizesCancellationThenSelectionBeforeOS(t *testing.T) {
	tests := []struct {
		name    string
		request evidence.Request
		want    error
	}{
		{name: "provider", request: evidence.Request{Provider: "codex", Profile: "default"}, want: evidence.ErrInvalidSelection},
		{name: "profile", request: evidence.Request{Provider: "cursor", Profile: "other"}, want: evidence.ErrInvalidSelection},
		{name: "account", request: evidence.Request{Provider: "cursor", Profile: "default", Account: "someone@example.test"}, want: evidence.ErrWrongAccount},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Given
			transport := &countingTransport{}
			adapter := New(Options{AuthFile: filepath.Join(t.TempDir(), "missing"), Client: &http.Client{Transport: transport}, goos: "darwin"})

			// When
			_, observeErr := adapter.Observe(t.Context(), test.request)
			_, bindingErr := adapter.CacheBinding(t.Context(), test.request)

			// Then
			if !errors.Is(observeErr, test.want) || !errors.Is(bindingErr, test.want) || errors.Is(observeErr, ErrUnsupported) || errors.Is(bindingErr, ErrUnsupported) || transport.calls != 0 {
				t.Fatalf("errors/calls = %v/%v/%d, want %v without I/O", observeErr, bindingErr, transport.calls, test.want)
			}
		})
	}

	// Given
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	adapter := New(Options{goos: "darwin"})

	// When
	_, err := adapter.Observe(ctx, evidence.Request{})

	// Then
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Observe() error = %v, want caller cancellation", err)
	}
}

func TestAdapterObserve_returnsCallerCancellationDuringSupplementalRPC(t *testing.T) {
	// Given
	planStarted := make(chan struct{})
	releasePlan := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch filepath.Base(r.URL.Path) {
		case "GetCurrentPeriodUsage":
			fmt.Fprint(w, fixture(t, "usage-zero.json"))
		case "GetPlanInfo":
			close(planStarted)
			<-releasePlan
		default:
			t.Fatalf("unexpected RPC after cancellation: %s", r.URL.Path)
		}
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		<-planStarted
		cancel()
		close(releasePlan)
	}()

	// When
	observation, err := testAdapter(t, server).Observe(ctx, cursorRequest())

	// Then
	if !errors.Is(err, context.Canceled) || observation.SchemaVersion != "" {
		t.Fatalf("Observe() = %+v, %v, want empty observation and caller cancellation", observation, err)
	}
}

func TestAdapterObserve_keepsPrimaryTimestampAndPartialDataOnOwnTimeout(t *testing.T) {
	// Given
	primaryObservedAt := fixtureNow.Add(time.Second)
	clockCalls := 0
	releaseSupplemental := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if filepath.Base(r.URL.Path) == "GetCurrentPeriodUsage" {
			fmt.Fprint(w, fixture(t, "usage-zero.json"))
			return
		}
		<-releaseSupplemental
	}))
	defer server.Close()
	adapter := New(Options{
		AuthFile: writeAuth(t, "synthetic-secret"),
		Endpoint: server.URL,
		Client:   server.Client(),
		Timeout:  20 * time.Millisecond,
		Now: func() time.Time {
			clockCalls++
			return fixtureNow.Add(time.Duration(clockCalls-1) * time.Second)
		},
		goos: "linux",
	})

	// When
	observation, err := adapter.Observe(t.Context(), cursorRequest())
	close(releaseSupplemental)

	// Then
	if err != nil || observation.Outcome != evidence.OutcomePartial || len(observation.Windows) == 0 || !observation.ObservedAt.Equal(primaryObservedAt) {
		t.Fatalf("Observe() = %+v, %v, want timestamp %s and partial primary data", observation, err, primaryObservedAt)
	}
}
