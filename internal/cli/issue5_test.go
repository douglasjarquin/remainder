package cli

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/douglasjarquin/remainder/internal/codex"
)

func TestExecuteWithCodexAdapter_rendersEveryOutputMode(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"account_id":"acct-test","rate_limit":{"primary_window":{"used_percent":100,"limit_window_seconds":18000},"secondary_window":{"used_percent":25,"limit_window_seconds":604800}}}`)
	}))
	defer server.Close()
	adapter := codex.New(codex.Options{AuthFile: writeCLIAuth(t), Endpoints: []string{server.URL}, Client: server.Client(), Timeout: time.Second})
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "compact", args: []string{"--provider", "codex", "--profile", "default"}, want: `provider="codex"`},
		{name: "json", args: []string{"--provider", "codex", "--profile", "default", "--format", "json"}, want: `"source":{"kind":"native_file_http","name":"codex_auth_json"}`},
		{name: "scalar", args: []string{"value", "--provider", "codex", "--profile", "default", "--window", "five_hour", "--field", "remaining"}, want: "0\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := ExecuteWithAdapter(t.Context(), test.args, &stdout, &stderr, "v0.1.0", adapter)
			if code != 0 || !strings.Contains(stdout.String(), test.want) || stderr.Len() != 0 {
				t.Fatalf("result: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
		})
	}
}

func TestCodexProcessEntryPoint_readsControlledSource(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"account_id":"acct-test","rate_limit":{"primary_window":{"used_percent":40,"limit_window_seconds":18000}}}`)
	}))
	defer server.Close()
	command := exec.Command(os.Args[0], "-test.run=^TestIssue5HelperProcess$")
	command.Env = []string{"REMAINDER_ISSUE5_HELPER=1", "REMAINDER_ISSUE5_AUTH=" + writeCLIAuth(t), "REMAINDER_ISSUE5_ENDPOINT=" + server.URL}
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil || stdout.String() != "60\n" || stderr.Len() != 0 {
		t.Fatalf("process result: err=%v stdout=%q stderr=%q", err, stdout.String(), stderr.String())
	}
}

func TestExecuteWithCodexAdapter_preservesUnknownAndRuntimeExitCodes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"account_id":"acct-test","rate_limit":{"primary_window":{"used_percent":40,"limit_window_seconds":18000}}}`)
	}))
	defer server.Close()
	adapter := codex.New(codex.Options{AuthFile: writeCLIAuth(t), Endpoints: []string{server.URL}, Client: server.Client(), Timeout: time.Second})
	var stdout, stderr bytes.Buffer
	code := ExecuteWithAdapter(t.Context(), []string{"value", "--provider", "codex", "--profile", "default", "--window", "weekly", "--field", "remaining"}, &stdout, &stderr, "test", adapter)
	if code != 2 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "undefined") {
		t.Fatalf("unknown scalar: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}

	missing := codex.New(codex.Options{AuthFile: filepath.Join(t.TempDir(), "missing"), Endpoints: []string{server.URL}, Client: server.Client(), Timeout: time.Second})
	stdout.Reset()
	stderr.Reset()
	code = ExecuteWithAdapter(t.Context(), []string{"--provider", "codex", "--profile", "default"}, &stdout, &stderr, "test", missing)
	if code != 1 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "authentication file is missing") {
		t.Fatalf("missing auth: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	stdout.Reset()
	stderr.Reset()
	code = ExecuteWithAdapter(ctx, []string{"--provider", "codex", "--profile", "default"}, &stdout, &stderr, "test", adapter)
	if code != 130 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "interrupted") {
		t.Fatalf("canceled request: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestIssue5HelperProcess(t *testing.T) {
	if os.Getenv("REMAINDER_ISSUE5_HELPER") != "1" {
		return
	}
	adapter := codex.New(codex.Options{AuthFile: os.Getenv("REMAINDER_ISSUE5_AUTH"), Endpoints: []string{os.Getenv("REMAINDER_ISSUE5_ENDPOINT")}, Timeout: time.Second})
	code := ExecuteWithAdapter(context.Background(), []string{"value", "--provider", "codex", "--profile", "default", "--window", "five_hour", "--field", "remaining"}, os.Stdout, os.Stderr, "test", adapter)
	os.Exit(code)
}

func writeCLIAuth(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "auth.json")
	if err := os.WriteFile(path, []byte(`{"tokens":{"access_token":"synthetic-secret","account_id":"acct-test"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
