//go:build darwin

package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/douglasjarquin/remainder/internal/evidence"
)

func TestExecute_native_Cursor_reportsUnsupportedBeforeSourceAccess_onDarwin(t *testing.T) {
	t.Setenv("CURSOR_CLI_CONFIG", t.TempDir()+"/missing-auth.json")
	var stdout, stderr bytes.Buffer

	code := Execute(t.Context(), []string{"--provider=cursor", "--profile=default", "--cache=off"}, &stdout, &stderr, "test")

	if code != 1 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "unsupported on this operating system") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestAllRequests_excludeCursor_onDarwin(t *testing.T) {
	want := []evidence.Provider{"codex", "claude", "grok"}
	if len(allRequests) != len(want) {
		t.Fatalf("allRequests=%+v", allRequests)
	}
	for index, request := range allRequests {
		if request.Provider != want[index] || request.Profile != "default" {
			t.Fatalf("allRequests[%d]=%+v, want %s/default", index, request, want[index])
		}
	}
}

func TestExecute_native_Cursor_rejectsInvalidProfileBeforeUnsupported_onDarwin(t *testing.T) {
	t.Setenv("CURSOR_CLI_CONFIG", t.TempDir()+"/missing-auth.json")
	var stdout, stderr bytes.Buffer

	code := Execute(t.Context(), []string{"--provider=cursor", "--profile=other", "--cache=off"}, &stdout, &stderr, "test")

	if code != 2 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "requires --provider cursor --profile default") || strings.Contains(stderr.String(), "unsupported on this operating system") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}
