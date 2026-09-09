package cli

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/douglasjarquin/remainder/internal/cache"
	"github.com/douglasjarquin/remainder/internal/codex"
	"github.com/douglasjarquin/remainder/internal/evidence"
	"github.com/spf13/cobra"
)

type runtimeAdapter struct {
	codex    codex.Adapter
	newStore func() (*cache.Store, error)
}

func defaultRuntimeAdapter() runtimeAdapter {
	return runtimeAdapter{codex: codex.Default(), newStore: func() (*cache.Store, error) { return cache.NewUserStore(cache.Options{}) }}
}

func (a runtimeAdapter) Observe(ctx context.Context, request evidence.Request) (evidence.Observation, error) {
	if request.Provider == "" {
		return unavailableAdapter{}.Observe(ctx, request)
	}
	return a.codex.Observe(ctx, request)
}

func (a runtimeAdapter) ObserveWithCache(ctx context.Context, request evidence.Request, policy cache.Policy) (cache.Result, error) {
	if request.Provider == "" {
		return cache.Result{}, ErrUnavailable
	}
	binding, err := a.codex.CacheBinding(ctx, request)
	if err != nil {
		if errors.Is(err, evidence.ErrInvalidSelection) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return cache.Result{}, err
		}
		if policy.Mode == cache.ModeOnly {
			return cache.Result{}, cache.ErrUnavailable
		}
		observation, fetchErr := a.codex.Observe(ctx, request)
		return cache.Result{Observation: observation}, fetchErr
	}
	store, err := a.newStore()
	if err != nil {
		if policy.Mode == cache.ModeOnly {
			return cache.Result{}, cache.ErrUnavailable
		}
		observation, fetchErr := a.codex.Observe(ctx, request)
		return cache.Result{Observation: observation, Warning: cache.WarningStorageUnavailable, CacheError: err}, fetchErr
	}
	return store.Resolve(ctx, binding, request.Account, policy, func(fetchContext context.Context) cache.FetchResult {
		observation, fetchErr := a.codex.Observe(fetchContext, request)
		return cache.FetchResult{Observation: observation, Failure: classifyFailure(fetchErr), RetryAt: retryAt(fetchErr), Err: fetchErr}
	})
}

func retryAt(err error) time.Time {
	if retry, ok := errors.AsType[*codex.RetryError](err); ok {
		return retry.RetryAt
	}
	return time.Time{}
}

func classifyFailure(err error) cache.FailureKind {
	switch {
	case err == nil:
		return cache.FailureNone
	case errors.Is(err, codex.ErrAuthorizationRejected):
		return cache.FailureRevoked
	case errors.Is(err, codex.ErrAccountMismatch), errors.Is(err, evidence.ErrWrongAccount):
		return cache.FailureAccountMismatch
	case errors.Is(err, codex.ErrTransient):
		return cache.FailureTransient
	default:
		return cache.FailurePermanent
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
