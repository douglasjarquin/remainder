package cursor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestReadMacKeychain_usesFixedBoundedCommandWithoutStderr(t *testing.T) {
	tests := []struct {
		name       string
		mode       string
		wantErr    bool
		wantOutput string
	}{
		{name: "token", mode: "token", wantOutput: "synthetic-secret\n"},
		{name: "oversized", mode: "oversized", wantErr: true},
		{name: "stderr is discarded", mode: "stderr", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := readMacKeychainWithRunner(t.Context(), func(command *exec.Cmd) error {
				wantArgs := []string{"/usr/bin/security", "find-generic-password", "-a", "cursor-user", "-w", "-s", "cursor-access-token"}
				if !slices.Equal(command.Args, wantArgs) || command.Path != "/usr/bin/security" || command.Stderr != io.Discard || command.WaitDelay != time.Second {
					t.Fatalf("command path/args/stderr/wait = %q/%q/%T/%s", command.Path, command.Args, command.Stderr, command.WaitDelay)
				}
				command.Path = os.Args[0]
				command.Args = []string{os.Args[0], "-test.run=TestCursorKeychainHelperProcess"}
				command.Env = append(os.Environ(), "REMAINDER_CURSOR_KEYCHAIN_HELPER="+test.mode)
				return command.Run()
			})

			if (err != nil) != test.wantErr || result != test.wantOutput || strings.Contains(fmt.Sprint(err), "stderr-secret") {
				t.Fatalf("readMacKeychainWithRunner() output length/error = %d, %v", len(result), err)
			}
		})
	}
}

func TestReadMacKeychain_cancelsAndWaitsForSyntheticHelper(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()

	_, err := readMacKeychainWithRunner(ctx, func(command *exec.Cmd) error {
		command.Path = os.Args[0]
		command.Args = []string{os.Args[0], "-test.run=TestCursorKeychainHelperProcess"}
		command.Env = append(os.Environ(), "REMAINDER_CURSOR_KEYCHAIN_HELPER=block")
		return command.Run()
	})

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("readMacKeychainWithRunner() error = %v", err)
	}
}

func TestCursorKeychainHelperProcess(t *testing.T) {
	switch os.Getenv("REMAINDER_CURSOR_KEYCHAIN_HELPER") {
	case "":
		return
	case "token":
		fmt.Fprintln(os.Stdout, "synthetic-secret")
		os.Exit(0)
	case "oversized":
		fmt.Fprint(os.Stdout, strings.Repeat("x", maxKeychainBytes+1))
		os.Exit(0)
	case "stderr":
		fmt.Fprintln(os.Stderr, "stderr-secret")
		os.Exit(7)
	case "block":
		time.Sleep(time.Minute)
	default:
		os.Exit(8)
	}
}
