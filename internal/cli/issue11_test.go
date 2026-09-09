package cli

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/douglasjarquin/remainder/internal/codex"
)

func TestCodexPaceProcessEntryPoint_emitsWeeklyPaceFromControlledSource(t *testing.T) {
	// Given
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"account_id":"acct-test","rate_limit":{"secondary_window":{"used_percent":58,"limit_window_seconds":604800,"reset_after_seconds":604800}}}`)
	}))
	defer server.Close()
	command := exec.Command(os.Args[0], "-test.run=^TestIssue11HelperProcess$")
	command.Env = []string{"REMAINDER_ISSUE11_HELPER=1", "REMAINDER_ISSUE11_AUTH=" + writeCLIAuth(t), "REMAINDER_ISSUE11_ENDPOINT=" + server.URL}
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr

	// When
	err := command.Run()

	// Then
	if err != nil || stdout.String() != "ahead\n" || stderr.Len() != 0 {
		t.Fatalf("process result: err=%v stdout=%q stderr=%q", err, stdout.String(), stderr.String())
	}
}

func TestIssue11HelperProcess(t *testing.T) {
	if os.Getenv("REMAINDER_ISSUE11_HELPER") != "1" {
		return
	}
	adapter := codex.New(codex.Options{AuthFile: os.Getenv("REMAINDER_ISSUE11_AUTH"), Endpoints: []string{os.Getenv("REMAINDER_ISSUE11_ENDPOINT")}, Timeout: time.Second})
	code := ExecuteWithAdapter(context.Background(), []string{"value", "--provider", "codex", "--profile", "default", "--window", "weekly", "--field", "pace"}, os.Stdout, os.Stderr, "test", adapter)
	os.Exit(code)
}
