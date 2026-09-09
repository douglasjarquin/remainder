package cli

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/douglasjarquin/remainder/internal/cache"
	"github.com/douglasjarquin/remainder/internal/claude"
	"github.com/douglasjarquin/remainder/internal/codex"
	"github.com/douglasjarquin/remainder/internal/evidence"
	"github.com/douglasjarquin/remainder/internal/grok"
	"github.com/spf13/cobra"
)

type runtimeAdapter struct {
	codex      nativeAdapter
	claude     nativeAdapter
	grok       nativeAdapter
	newStore   func() (*cache.Store, error)
	allTimeout time.Duration
}

func defaultRuntimeAdapter() runtimeAdapter {
	return runtimeAdapter{codex: codex.Default(), claude: claude.Default(), grok: grok.Default(), newStore: func() (*cache.Store, error) { return cache.NewUserStore(cache.Options{}) }}
}

func (a runtimeAdapter) Observe(ctx context.Context, request evidence.Request) (evidence.Observation, error) {
	if request.Provider == "" {
		return unavailableAdapter{}.Observe(ctx, request)
	}
	provider, err := a.provider(request)
	if err != nil {
		return evidence.Observation{}, err
	}
	return provider.Observe(ctx, request)
}

func (a runtimeAdapter) ObserveWithCache(ctx context.Context, request evidence.Request, policy cache.Policy) (cache.Result, error) {
	if request.Provider == "" {
		return cache.Result{}, ErrUnavailable
	}
	provider, err := a.provider(request)
	if err != nil {
		return cache.Result{}, err
	}
	binding, err := provider.CacheBinding(ctx, request)
	if err != nil {
		if errors.Is(err, evidence.ErrInvalidSelection) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return cache.Result{}, err
		}
		if policy.Mode == cache.ModeOnly {
			return cache.Result{}, cache.ErrUnavailable
		}
		observation, fetchErr := provider.Observe(ctx, request)
		return cache.Result{Observation: observation}, fetchErr
	}
	store, err := a.newStore()
	if err != nil {
		if policy.Mode == cache.ModeOnly {
			return cache.Result{}, cache.ErrUnavailable
		}
		observation, fetchErr := provider.Observe(ctx, request)
		return cache.Result{Observation: observation, Warning: cache.WarningStorageUnavailable, CacheError: err}, fetchErr
	}
	return store.Resolve(ctx, binding, request.Account, policy, func(fetchContext context.Context) cache.FetchResult {
		observation, fetchErr := provider.Observe(fetchContext, request)
		failure, retryAt := provider.Failure(fetchErr)
		return cache.FetchResult{Observation: observation, Failure: failure, RetryAt: retryAt, Err: fetchErr}
	})
}

type nativeAdapter interface {
	Observe(context.Context, evidence.Request) (evidence.Observation, error)
	CacheBinding(context.Context, evidence.Request) (cache.Binding, error)
	Failure(error) (cache.FailureKind, time.Time)
}

func (a runtimeAdapter) provider(request evidence.Request) (nativeAdapter, error) {
	switch request.Provider {
	case "codex":
		return a.codex, nil
	case "claude":
		return a.claude, nil
	case "grok":
		return a.grok, nil
	default:
		return nil, fmt.Errorf("%w: unsupported provider %q", evidence.ErrInvalidSelection, request.Provider)
	}
}

type cacheAdapter interface {
	ObserveWithCache(context.Context, evidence.Request, cache.Policy) (cache.Result, error)
}

func observe(cmd *cobra.Command, adapter Adapter, request evidence.Request, policy cache.Policy, policySet bool) (evidence.Observation, error) {
	if policy.Mode == cache.ModeOff {
		if _, ok := adapter.(cacheAdapter); !ok && policySet {
			return evidence.Observation{}, fmt.Errorf("%w: cache policy is unavailable for this adapter", ErrUsage)
		}
		return adapter.Observe(cmd.Context(), request)
	}
	cached, ok := adapter.(cacheAdapter)
	if !ok {
		if policySet {
			return evidence.Observation{}, fmt.Errorf("%w: cache policy is unavailable for this adapter", ErrUsage)
		}
		return adapter.Observe(cmd.Context(), request)
	}
	result, err := cached.ObserveWithCache(cmd.Context(), request, policy)
	if result.Warning != "" {
		fmt.Fprintf(cmd.ErrOrStderr(), "remainder: warning: %s\n", result.Warning)
	}
	return result.Observation, err
}

func readCachePolicy(cmd *cobra.Command) (cache.Policy, error) {
	cacheMode, err := cmd.Flags().GetString("cache")
	if err != nil {
		return cache.Policy{}, fmt.Errorf("%w: cache: %v", ErrUsage, err)
	}
	maxAge, err := cmd.Flags().GetDuration("max-age")
	if err != nil {
		return cache.Policy{}, fmt.Errorf("%w: max-age: %v", ErrUsage, err)
	}
	refresh, err := cmd.Flags().GetBool("refresh")
	if err != nil {
		return cache.Policy{}, fmt.Errorf("%w: refresh: %v", ErrUsage, err)
	}
	staleOnError, err := cmd.Flags().GetBool("stale-on-error")
	if err != nil {
		return cache.Policy{}, fmt.Errorf("%w: stale-on-error: %v", ErrUsage, err)
	}
	policy := cache.Policy{Mode: cache.Mode(cacheMode), MaxAge: maxAge, Refresh: refresh, StaleOnError: staleOnError}
	if maxAge < 0 {
		return cache.Policy{}, fmt.Errorf("%w: max-age cannot be negative", ErrUsage)
	}
	if policy.Mode == cache.ModeOff {
		if cmd.Flags().Changed("max-age") || refresh || staleOnError {
			return cache.Policy{}, fmt.Errorf("%w: cache off cannot use max-age, refresh, or stale-on-error", ErrUsage)
		}
		policy.MaxAge = 0
	}
	if policy.Mode == cache.ModeOnly && (refresh || staleOnError) {
		return cache.Policy{}, fmt.Errorf("%w: cache only cannot use refresh or stale-on-error", ErrUsage)
	}
	if err := policy.Validate(); err != nil {
		return cache.Policy{}, fmt.Errorf("%w: unsupported cache policy %q", ErrUsage, cacheMode)
	}
	return policy, nil
}
