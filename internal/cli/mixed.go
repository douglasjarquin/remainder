package cli

import (
	"context"
	json "encoding/json"
	"errors"
	"fmt"
	"io"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/douglasjarquin/go-toon"
	"github.com/douglasjarquin/remainder/internal/cache"
	"github.com/douglasjarquin/remainder/internal/evidence"
	"github.com/spf13/cobra"
)

const allCollectionTimeout = 15 * time.Second

var allRequests = []evidence.Request{
	{Provider: "codex", Profile: "default"},
	{Provider: "claude", Profile: "default"},
	{Provider: "grok", Profile: "default"},
}

func init() {
	if runtime.GOOS == "linux" || runtime.GOOS == "darwin" {
		allRequests = append(allRequests, evidence.Request{Provider: "cursor", Profile: "default"})
	}
}

type collectionResult struct {
	Request evidence.Request
	Result  cache.Result
	Err     error
}

type allAdapter interface {
	ObserveAll(context.Context, []evidence.Request, cache.Policy) []collectionResult
}

type mixedFailure struct {
	Provider evidence.Provider `json:"provider"`
	Profile  evidence.Profile  `json:"profile"`
	Message  string            `json:"message"`
}

type mixedReport struct {
	SchemaVersion string            `json:"schema_version"`
	Outcome       evidence.Outcome  `json:"outcome"`
	Observations  []json.RawMessage `json:"observations"`
	Failures      []mixedFailure    `json:"failures,omitempty"`
}

type mixedUnavailableError struct {
	failures []mixedFailure
}

func (e *mixedUnavailableError) Error() string {
	return ErrUnavailable.Error()
}

func (e *mixedUnavailableError) Unwrap() error {
	return ErrUnavailable
}

func (a runtimeAdapter) ObserveAll(ctx context.Context, requests []evidence.Request, policy cache.Policy) []collectionResult {
	timeout := a.allTimeout
	if timeout <= 0 {
		timeout = allCollectionTimeout
	}
	bounded, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	results := make([]collectionResult, len(requests))
	var wait sync.WaitGroup
	for index, request := range requests {
		wait.Go(func() {
			results[index] = collectionResult{Request: request}
			if policy.Mode == cache.ModeOff {
				results[index].Result.Observation, results[index].Err = a.Observe(bounded, request)
				return
			}
			results[index].Result, results[index].Err = a.ObserveWithCache(bounded, request, policy)
		})
	}
	wait.Wait()
	return results
}

func runAllReport(cmd *cobra.Command, adapter Adapter, opts options, now func() time.Time) error {
	for _, name := range []string{"provider", "profile", "account"} {
		if cmd.Flags().Changed(name) {
			return fmt.Errorf("%w: %w: --all cannot use --%s", ErrUsage, evidence.ErrInvalidSelection, name)
		}
	}
	collector, ok := adapter.(allAdapter)
	if !ok {
		return fmt.Errorf("%w: all-provider collection is unavailable for this adapter", ErrUsage)
	}
	requests := make([]evidence.Request, len(allRequests))
	for index, request := range allRequests {
		request.Window = opts.window
		request.Scope = opts.scope
		request.Freshness = opts.freshness
		requests[index] = request
	}
	results := collector.ObserveAll(cmd.Context(), requests, opts.cachePolicy)
	if err := cmd.Context().Err(); err != nil {
		return err
	}
	evaluatedAt := now()
	observations, failures, partial := assembleMixed(results, requests, opts, evaluatedAt)
	for _, request := range requests {
		result, found := resultFor(results, request)
		if found && result.Result.Warning != "" {
			fmt.Fprintf(cmd.ErrOrStderr(), "remainder: warning: %s/%s: %s\n", request.Provider, request.Profile, result.Result.Warning)
		}
	}
	if len(observations) == 0 {
		return &mixedUnavailableError{failures: failures}
	}
	outcome := evidence.OutcomeComplete
	if partial || len(failures) > 0 {
		outcome = evidence.OutcomePartial
	}
	switch opts.format {
	case "json":
		if err := writeMixedJSON(cmd.OutOrStdout(), observations, failures, outcome); err != nil {
			return err
		}
	case "toon":
		if err := writeMixedTOON(cmd.OutOrStdout(), observations, failures, outcome); err != nil {
			return err
		}
	default:
		if err := writeMixedCompact(cmd.OutOrStdout(), observations, failures, outcome, evaluatedAt); err != nil {
			return err
		}
	}
	if outcome == evidence.OutcomePartial {
		return ErrPartial
	}
	return nil
}

func assembleMixed(results []collectionResult, requests []evidence.Request, opts options, evaluatedAt time.Time) ([]evidence.Observation, []mixedFailure, bool) {
	observations := make([]evidence.Observation, 0, len(requests))
	failures := make([]mixedFailure, 0, len(requests))
	partial := false
	for _, request := range requests {
		result, found := resultFor(results, request)
		if !found {
			failures = append(failures, failureFor(request, errors.New("collection returned no result")))
			continue
		}
		if result.Err != nil {
			failures = append(failures, failureFor(request, result.Err))
			continue
		}
		observation := result.Result.Observation
		if observation.Outcome == evidence.OutcomeUnavailable {
			failures = append(failures, failureFor(request, ErrUnavailable))
			continue
		}
		if result.Result.FromCache {
			age := evaluatedAt.Sub(observation.ObservedAt)
			if age < 0 || (observation.Freshness == evidence.FreshFresh && age > opts.cachePolicy.MaxAge) {
				failures = append(failures, mixedFailure{Provider: request.Provider, Profile: request.Profile, Message: "cached quota evidence exceeded max-age"})
				continue
			}
		}
		if opts.freshness == evidence.FreshOnly && observation.Freshness != evidence.FreshFresh {
			failures = append(failures, failureFor(request, evidence.ErrStale))
			continue
		}
		observation = evidence.WithPace(observation, evaluatedAt)
		selected, err := observation.ForRequest(request)
		if err != nil {
			failures = append(failures, failureFor(request, err))
			continue
		}
		observations = append(observations, selected)
		partial = partial || selected.Outcome == evidence.OutcomePartial
	}
	return observations, failures, partial
}

func resultFor(results []collectionResult, request evidence.Request) (collectionResult, bool) {
	for _, result := range results {
		if result.Request.Provider == request.Provider && result.Request.Profile == request.Profile {
			return result, true
		}
	}
	return collectionResult{}, false
}

func failureFor(request evidence.Request, err error) mixedFailure {
	message := "quota is unavailable"
	switch {
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, cache.ErrLockTimeout):
		message = "quota collection timed out"
	case errors.Is(err, cache.ErrUnavailable):
		message = "cached quota evidence is unavailable"
	case errors.Is(err, evidence.ErrStale):
		message = "quota evidence is stale"
	case errors.Is(err, evidence.ErrWrongAccount):
		message = "quota account does not match"
	case errors.Is(err, evidence.ErrNoMatch):
		message = "requested quota evidence is unavailable"
	}
	return mixedFailure{Provider: request.Provider, Profile: request.Profile, Message: message}
}

func writeMixedTOON(writer io.Writer, observations []evidence.Observation, failures []mixedFailure, outcome evidence.Outcome) error {
	var encodedJSON []byte
	{
		var buf strings.Builder
		if err := writeMixedJSON(&buf, observations, failures, outcome); err != nil {
			return err
		}
		encodedJSON = []byte(strings.TrimSuffix(buf.String(), "\n"))
	}
	decoder := json.NewDecoder(strings.NewReader(string(encodedJSON)))
	decoder.UseNumber()
	var facts any
	if err := decoder.Decode(&facts); err != nil {
		return fmt.Errorf("decode mixed JSON for TOON: %w", err)
	}
	encoded, err := toon.Marshal(facts)
	if err != nil {
		return fmt.Errorf("render mixed TOON: %w", err)
	}
	if _, err := writer.Write(append(encoded, '\n')); err != nil {
		return fmt.Errorf("write mixed TOON: %w", err)
	}
	return nil
}

func writeMixedJSON(writer io.Writer, observations []evidence.Observation, failures []mixedFailure, outcome evidence.Outcome) error {
	raw := make([]json.RawMessage, 0, len(observations))
	for _, observation := range observations {
		encoded, err := evidence.RenderJSON(observation)
		if err != nil {
			return err
		}
		raw = append(raw, json.RawMessage(encoded))
	}
	encoded, err := json.Marshal(mixedReport{SchemaVersion: evidence.SchemaV1, Outcome: outcome, Observations: raw, Failures: failures})
	if err != nil {
		return fmt.Errorf("render mixed JSON: %w", err)
	}
	if _, err := writer.Write(append(encoded, '\n')); err != nil {
		return fmt.Errorf("write mixed JSON: %w", err)
	}
	return nil
}

func writeMixedCompact(writer io.Writer, observations []evidence.Observation, failures []mixedFailure, outcome evidence.Outcome, evaluatedAt time.Time) error {
	parts := []string{"schema=" + evidence.SchemaV1, "outcome=" + string(outcome)}
	entries := make([]string, 0, len(observations))
	for _, observation := range observations {
		entry, err := evidence.RenderCompact(observation, evaluatedAt)
		if err != nil {
			return err
		}
		entries = append(entries, strconv.Quote(entry))
	}
	parts = append(parts, "observations="+strings.Join(entries, ","))
	if len(failures) > 0 {
		entries = entries[:0]
		for _, failure := range failures {
			entries = append(entries, strconv.Quote(string(failure.Provider)+"/"+string(failure.Profile)+":"+failure.Message))
		}
		parts = append(parts, "failures="+strings.Join(entries, ","))
	}
	if _, err := fmt.Fprintln(writer, strings.Join(parts, " ")); err != nil {
		return fmt.Errorf("write mixed compact output: %w", err)
	}
	return nil
}
