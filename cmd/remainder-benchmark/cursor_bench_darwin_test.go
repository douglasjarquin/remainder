//go:build darwin

package main

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/douglasjarquin/remainder/internal/evidence"
)

func TestCursorBenchmark_runsControlledKeychainRefreshAndConfigCacheHitOnDarwin(t *testing.T) {
	// Given
	helper := buildControlledRefreshHelper(t)

	// When
	controlled, err := runControlledRefresh(helper, "cursor", 1, nil)
	// Then
	if err != nil {
		t.Fatal(err)
	}
	if len(controlled.Samples) != 1 || controlled.RequestCount != 3 {
		t.Fatalf("controlled result = %+v", controlled)
	}
	controlledSample := controlled.Samples[0]
	if controlledSample.ExitCode != 0 || controlledSample.Stdout != "60\n" || controlledSample.Stderr != "" || controlledSample.RequestCount != 3 || controlledSample.ControlledTLSRoundTripNS <= 0 || controlledSample.ElapsedNS <= controlledSample.ControlledTLSRoundTripNS {
		t.Fatalf("controlled sample = %+v", controlledSample)
	}

	configName, configBody := cacheHitAuth("cursor")
	if configName != filepath.Join("cursor", "cli-config.json") || string(configBody) != string(cursorBenchmarkConfig()) {
		t.Fatalf("cache fixture config = %q/%q", configName, configBody)
	}
	binary := buildCacheHitBinary(t)

	// When
	cached, err := runCacheHit(t.Context(), binary, "cursor", 1, time.Second, time.Minute, nil)
	// Then
	if err != nil {
		t.Fatal(err)
	}
	if len(cached.Samples) != 1 || cached.Samples[0].ExitCode != 0 || cached.Samples[0].Stdout != "42\n" || cached.Samples[0].Stderr != "" || cached.Samples[0].RequestCount != 0 || cached.IdentityBinding != evidence.IdentityUnknown || cached.Account != "" || cached.ObservedAt.IsZero() || cached.SandboxCleanup != "removed-owned-temporary-sandbox" {
		t.Fatalf("cache result = %+v", cached)
	}
}
