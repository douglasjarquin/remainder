package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/douglasjarquin/remainder/internal/benchmark"
	"github.com/douglasjarquin/remainder/internal/cache"
	"github.com/douglasjarquin/remainder/internal/evidence"
)

func TestCacheHitRun_recordsActualBinaryHit(t *testing.T) {
	binary := buildCacheHitBinary(t)

	for _, provider := range []string{"codex", "claude"} {
		t.Run(provider, func(t *testing.T) {
			result, err := runCacheHit(t.Context(), binary, provider, 3, time.Second, time.Minute, nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Samples) != 3 || result.Summary.Source != cacheHitSource || result.Summary.Workloads[cacheHitWorkload].Count != 3 || result.ObservedAt.IsZero() || result.Freshness != evidence.FreshFresh || result.IdentityBinding != evidence.IdentityHistorical || result.Account != "acct-benchmark" || result.ObservationAge != time.Second || result.MaxAge != time.Minute || result.Generation == "" || result.SandboxCleanup != "removed-owned-temporary-sandbox" {
				t.Fatalf("cache-hit result = %+v", result)
			}
			for _, value := range result.Samples {
				if value.Source != cacheHitSource || value.ExitCode != 0 || value.Stdout != "42\n" || value.Stderr != "" || value.StdoutBytes != 3 || value.StderrBytes != 0 || value.SubprocessCount != 1 || value.RequestCount != 0 || value.ElapsedNS <= 0 || value.OutputStatus != "pass" {
					t.Fatalf("cache-hit sample = %+v", value)
				}
			}
			if result.Provenance.Workload != "cache-hit-provenance" || result.Provenance.ExitCode != 0 || result.Provenance.SubprocessCount != 1 || result.Provenance.RequestCount != 0 || result.Provenance.Stdout == "" || result.Provenance.Stderr != "" || result.Provenance.OutputStatus != "pass" {
				t.Fatalf("cache-hit provenance = %+v", result.Provenance)
			}
		})
	}
}

func TestCacheHitObjective_appliesOnlyToAppleSilicon(t *testing.T) {
	measured := benchmark.Summary{P95NS: 10_000_001}
	if err := cacheHitObjectiveFailure(measured, "darwin", "arm64", 10_000_000); err == nil {
		t.Fatal("Apple Silicon objective miss unexpectedly passed")
	}
	if err := cacheHitObjectiveFailure(measured, "linux", "arm64", 10_000_000); err != nil {
		t.Fatalf("non-reference host failed: %v", err)
	}
}

func TestCacheHitRun_rejectsExpiredSeedWithoutReadingOAuth(t *testing.T) {
	binary := buildCacheHitBinary(t)

	result, err := runCacheHit(t.Context(), binary, "codex", 1, time.Hour, time.Second, nil)
	if err == nil {
		t.Fatal("expired cache hit unexpectedly passed")
	}
	if len(result.Samples) != 1 || result.Samples[0].ExitCode == 0 || result.Samples[0].Stdout != "" || result.Samples[0].RequestCount != 0 || !strings.Contains(result.Samples[0].Stderr, "authentication file is malformed") || result.SandboxCleanup != "removed-owned-temporary-sandbox" {
		t.Fatalf("cache-hit result = %+v, error = %v", result, err)
	}
}

func TestCacheHitSeed_grokPreservesCreditsAndUnknownIdentityOnReuse(t *testing.T) {
	// Given
	authName, authBody := cacheHitAuth("grok")
	authPath := filepath.Join(t.TempDir(), authName)
	if err := os.WriteFile(authPath, authBody, 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.September, 9, 16, 0, 0, 0, time.UTC)
	binding, observation, window, err := cacheHitSeed(t.Context(), "grok", authPath, now.Add(-time.Second))
	if err != nil {
		t.Fatal(err)
	}
	store := cache.New(filepath.Join(t.TempDir(), "cache"), cache.Options{Now: func() time.Time { return now }})
	if _, err := store.Put(t.Context(), binding, observation); err != nil {
		t.Fatal(err)
	}

	// When
	result, err := store.Resolve(t.Context(), binding, "", cache.Policy{Mode: cache.ModeOnly, MaxAge: time.Minute}, nil)

	// Then
	if err != nil || !result.FromCache || binding.Provider != "grok" || binding.SourceKind != "native_file_http" || binding.SourceName != "grok_auth_json" || window != "credits" || result.Observation.Account.Binding != evidence.IdentityUnknown || result.Observation.Account.LastObserved != "" {
		t.Fatalf("result=%+v binding=%+v window=%q error=%v", result, binding, window, err)
	}
	remaining, err := evidence.SelectValue(result.Observation, evidence.ValueRequest{Provider: "grok", Profile: "default", Window: "credits", Field: evidence.FieldRemaining}, evidence.FreshOnly)
	if err != nil || remaining != "42\n" {
		t.Fatalf("remaining=%q error=%v", remaining, err)
	}
}

func TestCacheRootForOS_usesOwnedPlatformLocation(t *testing.T) {
	tests := []struct {
		goos string
		want string
	}{
		{goos: "darwin", want: filepath.Join("/owned/home", "Library", "Caches", "remainder", "v1")},
		{goos: "linux", want: filepath.Join("/owned/cache", "remainder", "v1")},
	}
	for _, test := range tests {
		t.Run(test.goos, func(t *testing.T) {
			got, err := cacheRootForOS(test.goos, "/owned/home", "/owned/cache")
			if err != nil || got != test.want {
				t.Fatalf("cache root = %q, error = %v, want %q", got, err, test.want)
			}
		})
	}
	if _, err := cacheRootForOS("plan9", "/owned/home", "/owned/cache"); err == nil {
		t.Fatal("unsupported platform unexpectedly succeeded")
	}
}

func buildCacheHitBinary(t *testing.T) string {
	t.Helper()
	output, err := exec.Command("go", "env", "GOMOD").Output()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Dir(strings.TrimSpace(string(output)))
	binary := filepath.Join(t.TempDir(), "remainder")
	command := exec.Command("go", "build", "-trimpath", "-o", binary, "./cmd/remainder")
	command.Dir = root
	command.Env = append(os.Environ(), "CGO_ENABLED=0", "GOTOOLCHAIN=local", "GOPROXY=off")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build cache-hit binary: %v\n%s", err, output)
	}
	info, err := os.Stat(binary)
	if err != nil || info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("cache-hit binary: info=%v error=%v", info, err)
	}
	return binary
}

func TestCacheHitRun_rejectsInvalidInput(t *testing.T) {
	_, err := runCacheHit(t.Context(), "missing", "codex", 0, 0, 0, nil)
	if !errors.Is(err, benchmark.ErrNoSamples) {
		t.Fatalf("error = %v", err)
	}
}
