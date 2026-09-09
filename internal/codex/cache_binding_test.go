package codex

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/douglasjarquin/remainder/internal/evidence"
)

func TestAdapter_CacheBinding_usesMetadataWithoutParsingOAuth(t *testing.T) {
	// Given
	path := filepath.Join(t.TempDir(), "auth.json")
	if err := os.WriteFile(path, []byte("not OAuth JSON"), 0o600); err != nil {
		t.Fatal(err)
	}
	adapter := New(Options{AuthFile: path})
	request := evidence.Request{Provider: "codex", Profile: "default"}

	// When
	binding, err := adapter.CacheBinding(t.Context(), request)

	// Then
	if err != nil || binding.CredentialFingerprint == "" {
		t.Fatalf("binding = %+v, error = %v", binding, err)
	}
	if _, err := adapter.Observe(t.Context(), request); err == nil {
		t.Fatal("Observe() succeeded with malformed OAuth JSON")
	}
}

func TestAdapter_CacheBinding_changesWithSelectedAuthSource(t *testing.T) {
	// Given
	firstPath := filepath.Join(t.TempDir(), "auth.json")
	secondPath := filepath.Join(t.TempDir(), "auth.json")
	for _, path := range []string{firstPath, secondPath} {
		if err := os.WriteFile(path, []byte(`{"tokens":{}}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	request := evidence.Request{Provider: "codex", Profile: "default"}

	// When
	first, firstErr := New(Options{AuthFile: firstPath}).CacheBinding(t.Context(), request)
	second, secondErr := New(Options{AuthFile: secondPath}).CacheBinding(t.Context(), request)

	// Then
	if firstErr != nil || secondErr != nil || first.CredentialFingerprint == second.CredentialFingerprint {
		t.Fatalf("first = %+v (%v), second = %+v (%v)", first, firstErr, second, secondErr)
	}
}
