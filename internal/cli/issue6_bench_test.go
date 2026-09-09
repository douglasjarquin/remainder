package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/douglasjarquin/remainder/internal/cache"
	"github.com/douglasjarquin/remainder/internal/codex"
	"github.com/douglasjarquin/remainder/internal/evidence"
)

func BenchmarkExecuteCacheHit(b *testing.B) {
	root := b.TempDir()
	home := filepath.Join(root, "home")
	codexHome := filepath.Join(root, "codex-home")
	cacheHome := filepath.Join(root, "cache-home")
	for _, directory := range []string{home, codexHome, cacheHome} {
		if err := os.Mkdir(directory, 0o700); err != nil {
			b.Fatal(err)
		}
	}
	b.Setenv("HOME", home)
	b.Setenv("CODEX_HOME", codexHome)
	b.Setenv("XDG_CACHE_HOME", cacheHome)
	authPath := filepath.Join(codexHome, "auth.json")
	if err := os.WriteFile(authPath, []byte("{"), 0o600); err != nil {
		b.Fatal(err)
	}
	binding, err := codex.New(codex.Options{AuthFile: authPath}).CacheBinding(b.Context(), evidence.Request{Provider: "codex", Profile: "default"})
	if err != nil {
		b.Fatal(err)
	}
	store, err := cache.NewUserStore(cache.Options{})
	if err != nil {
		b.Fatal(err)
	}
	observedAt := time.Now().UTC().Add(-time.Second)
	if _, err := store.Put(b.Context(), binding, cacheObservation(observedAt)); err != nil {
		b.Fatal(err)
	}
	args := []string{"value", "--provider", "codex", "--profile", "default", "--window", "five_hour", "--field", "remaining", "--cache", "auto", "--max-age", "24h"}
	var stdout, stderr bytes.Buffer
	code := 0
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		stdout.Reset()
		stderr.Reset()
		code = Execute(b.Context(), args, &stdout, &stderr, "test")
	}
	b.StopTimer()
	b.ReportMetric(0, "requests/op")
	if code != 0 || stdout.String() != "60\n" || stderr.Len() != 0 {
		b.Fatalf("result: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}
