package grok

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/douglasjarquin/remainder/internal/evidence"
)

func TestCacheBindingIsolation(t *testing.T) {
	first := filepath.Join(t.TempDir(), "auth.json")
	second := filepath.Join(t.TempDir(), "auth.json")
	for _, path := range []string{first, second} {
		if err := os.WriteFile(path, []byte("not parsed by CacheBinding"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	one, firstErr := New(Options{AuthFile: first}).CacheBinding(t.Context(), request())
	two, secondErr := New(Options{AuthFile: second}).CacheBinding(t.Context(), request())
	if firstErr != nil || secondErr != nil {
		t.Fatalf("binding errors = %v, %v", firstErr, secondErr)
	}
	if one.CredentialFingerprint == two.CredentialFingerprint || one.Provider != "grok" || one.Profile != "default" || one.ResponseBoundary != "grok_credits_config" || one.SourceKind != "native_file_http" || one.SourceName != "grok_auth_json" {
		t.Fatalf("bindings = %+v, %+v", one, two)
	}
	if err := one.Validate(); err != nil {
		t.Fatalf("invalid binding: %v", err)
	}
	if _, err := New(Options{AuthFile: first}).CacheBinding(t.Context(), evidence.Request{Provider: "codex", Profile: "default"}); !errors.Is(err, evidence.ErrInvalidSelection) {
		t.Fatalf("selection error = %v", err)
	}
}
