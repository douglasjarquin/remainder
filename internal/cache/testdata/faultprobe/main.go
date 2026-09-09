package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/douglasjarquin/remainder/internal/cache"
	"github.com/douglasjarquin/remainder/internal/evidence"
)

var (
	probeNow  = time.Date(2026, time.September, 9, 12, 0, 0, 0, time.UTC)
	probeRoot = "/cache/remainder/v1"
	probeBind = cache.Binding{Provider: "codex", Profile: "default", ResponseBoundary: "usage", SourceKind: "native_file_http", SourceName: "codex_auth_json", CredentialFingerprint: "fingerprint"}
)

func main() {
	if len(os.Args) != 2 {
		fail("usage: fault-probe seed|readonly|full")
	}
	switch os.Args[1] {
	case "seed":
		seed()
	case "readonly":
		readOnly()
	case "full":
		full()
	default:
		fail("usage: fault-probe seed|readonly|full")
	}
}

func seed() {
	store := newStore()
	if _, err := store.Put(context.Background(), probeBind, observation(probeNow.Add(-time.Minute))); err != nil {
		fail("seed: %v", err)
	}
	printSnapshot("seed", store.SnapshotPath(probeBind))
}

func readOnly() {
	store := newStore()
	path := store.SnapshotPath(probeBind)
	before := mustRead(path)
	result, err := store.Resolve(context.Background(), probeBind, "", refreshPolicy(), func(context.Context) cache.FetchResult {
		return cache.FetchResult{Observation: observation(probeNow)}
	})
	if err != nil {
		fail("readonly resolve: %v", err)
	}
	if result.Warning != cache.WarningStorageUnavailable || !errors.Is(result.CacheError, syscall.EROFS) {
		fail("readonly result: warning=%q cache_error=%v", result.Warning, result.CacheError)
	}
	if !result.Observation.ObservedAt.Equal(probeNow) {
		fail("readonly live observation: %s", result.Observation.ObservedAt)
	}
	if after := mustRead(path); string(after) != string(before) {
		fail("readonly cache record changed")
	}
	printSnapshot("readonly", path)
	fmt.Println("RESULT=readonly-storage-unavailable-old-record-preserved")
}

func full() {
	store := newStore()
	if _, err := store.Put(context.Background(), probeBind, observation(probeNow.Add(-time.Minute))); err != nil {
		fail("full seed: %v", err)
	}
	path := store.SnapshotPath(probeBind)
	before := mustRead(path)
	filler, err := os.Create(filepath.Join(probeRoot, "owned-filler"))
	if err != nil {
		fail("full create filler: %v", err)
	}
	block := make([]byte, 4096)
	for {
		if _, err := filler.Write(block); err != nil {
			if !errors.Is(err, syscall.ENOSPC) {
				fail("full fill: %v", err)
			}
			break
		}
	}
	if err := filler.Close(); err != nil {
		fail("full close filler: %v", err)
	}
	result, err := store.Resolve(context.Background(), probeBind, "", refreshPolicy(), func(context.Context) cache.FetchResult {
		return cache.FetchResult{Observation: observation(probeNow)}
	})
	if err != nil {
		fail("full resolve: %v", err)
	}
	if result.Warning != cache.WarningStorageUnavailable || !errors.Is(result.CacheError, syscall.ENOSPC) {
		fail("full result: warning=%q cache_error=%v", result.Warning, result.CacheError)
	}
	if after := mustRead(path); string(after) != string(before) {
		fail("full cache record changed")
	}
	printSnapshot("full", path)
	fmt.Println("RESULT=full-storage-unavailable-old-record-preserved")
}

func newStore() *cache.Store {
	return cache.New(probeRoot, cache.Options{Now: func() time.Time { return probeNow }})
}

func refreshPolicy() cache.Policy {
	return cache.Policy{Mode: cache.ModeAuto, MaxAge: time.Second, Refresh: true}
}

func observation(observedAt time.Time) evidence.Observation {
	return evidence.Observation{
		SchemaVersion: evidence.SchemaV1,
		Provider:      "codex",
		Profile:       "default",
		Account:       evidence.AccountIdentity{LastObserved: "acct-test", Binding: evidence.IdentityVerified},
		Source:        evidence.SourceIdentity{Kind: "native_file_http", Name: "codex_auth_json"},
		ObservedAt:    observedAt,
		Freshness:     evidence.FreshFresh,
		Outcome:       evidence.OutcomeComplete,
	}
}

func mustRead(path string) []byte {
	data, err := os.ReadFile(path)
	if err != nil {
		fail("read snapshot: %v", err)
	}
	return data
}

func printSnapshot(prefix, path string) {
	data := mustRead(path)
	fmt.Printf("%s_snapshot_sha256=%x\n", prefix, sha256.Sum256(data))
}

func fail(format string, values ...any) {
	fmt.Fprintf(os.Stderr, "FAIL: "+format+"\n", values...)
	os.Exit(1)
}
