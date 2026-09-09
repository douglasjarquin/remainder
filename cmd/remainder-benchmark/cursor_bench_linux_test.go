//go:build linux

package main

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/douglasjarquin/remainder/internal/evidence"
)

func TestCursorBenchmark_runsControlledRefreshAndCacheHitOnLinux(t *testing.T) {
	helper := buildControlledRefreshHelper(t)
	controlled, err := runControlledRefresh(helper, "cursor", 1, nil)
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

	authName, authBody := cacheHitAuth("cursor")
	if authName != filepath.Join("cursor", "auth.json") || string(authBody) != "{" {
		t.Fatalf("cache fixture auth = %q/%q", authName, authBody)
	}
	binary := buildCacheHitBinary(t)
	cached, err := runCacheHit(t.Context(), binary, "cursor", 1, time.Second, time.Minute, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(cached.Samples) != 1 || cached.Samples[0].ExitCode != 0 || cached.Samples[0].Stdout != "42\n" || cached.Samples[0].Stderr != "" || cached.Samples[0].RequestCount != 0 || cached.IdentityBinding != evidence.IdentityUnknown || cached.Account != "" || cached.SandboxCleanup != "removed-owned-temporary-sandbox" {
		t.Fatalf("cache result = %+v", cached)
	}
}
