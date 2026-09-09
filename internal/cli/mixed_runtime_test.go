package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/douglasjarquin/remainder/internal/cache"
	"github.com/douglasjarquin/remainder/internal/evidence"
)

func TestRuntimeAdapter_ObserveAllStartsThreeOperationsConcurrently(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	started := make(chan evidence.Provider, 3)
	release := make(chan struct{})
	adapter := runtimeAdapter{
		codex:  blockingNativeAdapter{observation: providerObservation(fixedCLINow(), "codex"), started: started, release: release},
		claude: blockingNativeAdapter{observation: providerObservation(fixedCLINow(), "claude"), started: started, release: release},
		grok:   blockingNativeAdapter{observation: providerObservation(fixedCLINow(), "grok"), started: started, release: release},
	}
	completed := make(chan []collectionResult, 1)
	go func() {
		completed <- adapter.ObserveAll(ctx, allRequests, cache.Policy{Mode: cache.ModeOff})
	}()

	seen := make(map[evidence.Provider]bool)
	for range 3 {
		select {
		case provider := <-started:
			seen[provider] = true
		case <-ctx.Done():
			t.Fatal("three provider operations did not start concurrently")
		}
	}
	close(release)
	results := <-completed

	if len(results) != 3 || !seen["codex"] || !seen["claude"] || !seen["grok"] {
		t.Fatalf("started=%v results=%+v", seen, results)
	}
}

func TestExecuteAll_sharedDeadlineRetainsCompletedProvider(t *testing.T) {
	block := make(chan struct{})
	adapter := runtimeAdapter{
		codex:      blockingNativeAdapter{observation: providerObservation(fixedCLINow(), "codex")},
		claude:     blockingNativeAdapter{provider: "claude", release: block},
		grok:       blockingNativeAdapter{provider: "grok", release: block},
		allTimeout: 20 * time.Millisecond,
	}
	var stdout, stderr bytes.Buffer

	code := executeWithAdapterAt(t.Context(), []string{"--all", "--cache=off", "--format=json"}, &stdout, &stderr, "test", fixedCLINow(), adapter)

	if code != 3 || !strings.Contains(stdout.String(), `"provider":"codex"`) || !strings.Contains(stdout.String(), `"message":"quota collection timed out"`) || stderr.String() != "remainder: partial evidence\n" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestExecuteAll_cancellationWaitsForOwnedOperationsAndWritesNoStdout(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	started := make(chan evidence.Provider, 3)
	block := make(chan struct{})
	var active atomic.Int32
	adapter := runtimeAdapter{
		codex:  blockingNativeAdapter{provider: "codex", started: started, release: block, active: &active},
		claude: blockingNativeAdapter{provider: "claude", started: started, release: block, active: &active},
		grok:   blockingNativeAdapter{provider: "grok", started: started, release: block, active: &active},
	}
	go func() {
		for range 3 {
			<-started
		}
		cancel()
	}()
	var stdout, stderr bytes.Buffer

	code := executeWithAdapterAt(ctx, []string{"--all", "--cache=off"}, &stdout, &stderr, "test", fixedCLINow(), adapter)

	if code != 130 || stdout.Len() != 0 || active.Load() != 0 || stderr.String() != "remainder: interrupted\n" {
		t.Fatalf("code=%d active=%d stdout=%q stderr=%q", code, active.Load(), stdout.String(), stderr.String())
	}
}

type blockingNativeAdapter struct {
	provider    evidence.Provider
	observation evidence.Observation
	err         error
	started     chan<- evidence.Provider
	release     <-chan struct{}
	active      *atomic.Int32
}

func (a blockingNativeAdapter) Observe(ctx context.Context, request evidence.Request) (evidence.Observation, error) {
	provider := a.provider
	if provider == "" {
		provider = request.Provider
	}
	if a.active != nil {
		a.active.Add(1)
		defer a.active.Add(-1)
	}
	if a.started != nil {
		a.started <- provider
	}
	if a.release != nil {
		select {
		case <-a.release:
		case <-ctx.Done():
			return evidence.Observation{}, ctx.Err()
		}
	}
	return a.observation, a.err
}

func (a blockingNativeAdapter) CacheBinding(context.Context, evidence.Request) (cache.Binding, error) {
	return cache.Binding{}, errors.New("cache binding is not used")
}

func (a blockingNativeAdapter) Failure(error) (cache.FailureKind, time.Time) {
	return cache.FailurePermanent, time.Time{}
}
