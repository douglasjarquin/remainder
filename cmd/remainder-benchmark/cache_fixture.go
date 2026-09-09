package main

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/douglasjarquin/remainder/internal/cache"
	"github.com/douglasjarquin/remainder/internal/claude"
	"github.com/douglasjarquin/remainder/internal/codex"
	"github.com/douglasjarquin/remainder/internal/cursor"
	"github.com/douglasjarquin/remainder/internal/evidence"
	"github.com/douglasjarquin/remainder/internal/grok"
)

func cacheHitAuth(provider string) (string, []byte) {
	if provider == "claude" {
		return ".credentials.json", []byte("{")
	}
	if provider == "cursor" {
		return filepath.Join("cursor", "auth.json"), []byte("{")
	}
	return "auth.json", []byte("{")
}

func cacheHitSeed(ctx context.Context, provider, authPath string, observedAt time.Time) (cache.Binding, evidence.Observation, string, error) {
	request := evidence.Request{Provider: evidence.Provider(provider), Profile: "default"}
	if provider == "cursor" {
		binding, err := cursor.New(cursor.Options{AuthFile: authPath}).CacheBinding(ctx, request)
		if err != nil {
			return cache.Binding{}, evidence.Observation{}, "", fmt.Errorf("derive cache-hit binding: %w", err)
		}
		amount := evidence.JSONNumber("42")
		return binding, evidence.Observation{
			SchemaVersion: evidence.SchemaV1,
			Provider:      "cursor",
			Profile:       "default",
			Account:       evidence.AccountIdentity{Binding: evidence.IdentityUnknown},
			Source:        evidence.SourceIdentity{Kind: binding.SourceKind, Name: binding.SourceName},
			ObservedAt:    observedAt,
			Freshness:     evidence.FreshFresh,
			Outcome:       evidence.OutcomeComplete,
			Windows: []evidence.Window{{
				ID: "included_usage", Scope: evidence.ScopeAccount, Unit: "percent",
				Limits: []evidence.Limit{{ID: "included_usage_remaining", Field: evidence.FieldRemaining, Value: evidence.Value{State: evidence.ValueDefined, Amount: &amount}}},
			}},
		}, "included_usage", nil
	}
	if provider == "grok" {
		binding, err := grok.New(grok.Options{AuthFile: authPath}).CacheBinding(ctx, request)
		if err != nil {
			return cache.Binding{}, evidence.Observation{}, "", fmt.Errorf("derive cache-hit binding: %w", err)
		}
		amount := evidence.JSONNumber("42")
		return binding, evidence.Observation{
			SchemaVersion: evidence.SchemaV1,
			Provider:      "grok",
			Profile:       "default",
			Account:       evidence.AccountIdentity{Binding: evidence.IdentityUnknown},
			Source:        evidence.SourceIdentity{Kind: binding.SourceKind, Name: binding.SourceName},
			ObservedAt:    observedAt,
			Freshness:     evidence.FreshFresh,
			Outcome:       evidence.OutcomeComplete,
			Windows: []evidence.Window{{
				ID: "credits", Scope: evidence.ScopeAccount, Unit: "percent",
				Limits: []evidence.Limit{{ID: "credits_remaining", Field: evidence.FieldRemaining, Value: evidence.Value{State: evidence.ValueDefined, Amount: &amount}}},
			}},
		}, "credits", nil
	}
	var binding cache.Binding
	var err error
	if provider == "claude" {
		binding, err = claude.New(claude.Options{AuthFile: authPath}).CacheBinding(ctx, request)
	} else {
		binding, err = codex.New(codex.Options{AuthFile: authPath}).CacheBinding(ctx, request)
	}
	if err != nil {
		return cache.Binding{}, evidence.Observation{}, "", fmt.Errorf("derive cache-hit binding: %w", err)
	}
	amount := evidence.JSONNumber("42")
	return binding, evidence.Observation{
		SchemaVersion: evidence.SchemaV1,
		Provider:      evidence.Provider(provider),
		Profile:       "default",
		Account:       evidence.AccountIdentity{LastObserved: "acct-benchmark", Binding: evidence.IdentityVerified},
		Source:        evidence.SourceIdentity{Kind: binding.SourceKind, Name: binding.SourceName},
		ObservedAt:    observedAt,
		Freshness:     evidence.FreshFresh,
		Outcome:       evidence.OutcomeComplete,
		Windows: []evidence.Window{{
			ID: "five_hour", Scope: evidence.ScopeAccount, Unit: "percent",
			Limits: []evidence.Limit{{ID: "five_hour_remaining", Field: evidence.FieldRemaining, Value: evidence.Value{State: evidence.ValueDefined, Amount: &amount}}},
		}},
	}, "five_hour", nil
}

func cacheHitIdentityBinding(provider string) evidence.IdentityBinding {
	if provider == "grok" || provider == "cursor" {
		return evidence.IdentityUnknown
	}
	return evidence.IdentityHistorical
}
