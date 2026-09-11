package cli

import (
	"reflect"
	"testing"

	"github.com/douglasjarquin/remainder/internal/claude"
	"github.com/douglasjarquin/remainder/internal/cursor"
)

func TestAuthorizeKeychainPrompt_wiresClaudeAdapterRegardlessOfHostOS(t *testing.T) {
	provider := claude.New(claude.Options{AuthFile: "synthetic-missing.json"})
	adapter := runtimeAdapter{claude: provider}

	authorized := authorizeKeychainPrompt(adapter, true)

	rt, ok := authorized.(runtimeAdapter)
	if !ok {
		t.Fatalf("authorizeKeychainPrompt() returned %T, want runtimeAdapter", authorized)
	}
	claudeAdapter, ok := rt.claude.(claude.Adapter)
	if !ok {
		t.Fatalf("rt.claude = %T, want claude.Adapter", rt.claude)
	}
	value := reflect.ValueOf(claudeAdapter)
	allowField := value.FieldByName("allowKeychainPrompt")
	if !allowField.IsValid() || !allowField.Bool() {
		t.Fatalf("allowKeychainPrompt = %v, want true after authorizeKeychainPrompt", allowField)
	}
	readerField := value.FieldByName("keychainReader")
	if !readerField.IsValid() || readerField.IsNil() {
		t.Fatal("keychainReader is nil after authorizeKeychainPrompt")
	}
}

func TestAuthorizeKeychainPrompt_wiresCursorAdapterRegardlessOfHostOS(t *testing.T) {
	provider := cursor.New(cursor.Options{AuthFile: "synthetic-missing.json"})
	adapter := runtimeAdapter{cursor: provider}

	authorized := authorizeKeychainPrompt(adapter, true)

	rt, ok := authorized.(runtimeAdapter)
	if !ok {
		t.Fatalf("authorizeKeychainPrompt() returned %T, want runtimeAdapter", authorized)
	}
	cursorAdapter, ok := rt.cursor.(cursor.Adapter)
	if !ok {
		t.Fatalf("rt.cursor = %T, want cursor.Adapter", rt.cursor)
	}
	value := reflect.ValueOf(cursorAdapter)
	allowField := value.FieldByName("allowKeychainPrompt")
	if !allowField.IsValid() || !allowField.Bool() {
		t.Fatalf("allowKeychainPrompt = %v, want true after authorizeKeychainPrompt", allowField)
	}
}

func TestAuthorizeKeychainPrompt_leavesAdapterUnchangedWhenNotAllowed(t *testing.T) {
	provider := claude.New(claude.Options{AuthFile: "synthetic-missing.json"})
	adapter := runtimeAdapter{claude: provider}

	authorized := authorizeKeychainPrompt(adapter, false)

	rt, ok := authorized.(runtimeAdapter)
	if !ok {
		t.Fatalf("authorizeKeychainPrompt() returned %T, want runtimeAdapter", authorized)
	}
	claudeAdapter, ok := rt.claude.(claude.Adapter)
	if !ok {
		t.Fatalf("rt.claude = %T, want claude.Adapter", rt.claude)
	}
	allowField := reflect.ValueOf(claudeAdapter).FieldByName("allowKeychainPrompt")
	if !allowField.IsValid() || allowField.Bool() {
		t.Fatalf("allowKeychainPrompt = %v, want false when not allowed", allowField)
	}
}
