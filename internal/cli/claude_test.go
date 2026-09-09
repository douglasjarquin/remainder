package cli

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/douglasjarquin/remainder/internal/cache"
	"github.com/douglasjarquin/remainder/internal/claude"
	"github.com/douglasjarquin/remainder/internal/codex"
	"github.com/douglasjarquin/remainder/internal/evidence"
)

func TestExecute_native_Claude_supports_compact_JSON_and_scalar(t *testing.T) {
	adapter := claudeRuntimeAdapter(t).adapter
	for _, tt := range []struct {
		name string
		args []string
		want string
	}{
		{"compact", []string{"--provider", "claude", "--profile", "default", "--cache", "off"}, `provider="claude" profile="default"`},
		{"JSON", []string{"--provider", "claude", "--profile", "default", "--cache", "off", "--format", "json"}, `"provider":"claude"`},
		{"scalar zero", []string{"value", "--provider", "claude", "--profile", "default", "--window", "five_hour", "--field", "used", "--cache", "off"}, "0\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := executeWithAdapterAt(t.Context(), tt.args, &stdout, &stderr, "test", fixedCLINow(), adapter)
			if code != 0 || !strings.Contains(stdout.String(), tt.want) || stderr.Len() != 0 {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
		})
	}
}

func TestExecute_native_Claude_missing_file_is_operational_failure(t *testing.T) {
	adapter := runtimeAdapter{codex: codex.Default(), claude: claude.New(claude.Options{AuthFile: filepath.Join(t.TempDir(), "missing")}), newStore: func() (*cache.Store, error) { return cache.NewUserStore(cache.Options{}) }}
	var stdout, stderr bytes.Buffer
	code := executeWithAdapterAt(t.Context(), []string{"value", "--provider", "claude", "--profile", "default", "--window", "weekly", "--field", "remaining", "--cache", "off"}, &stdout, &stderr, "test", fixedCLINow(), adapter)
	if code != 1 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "missing") || strings.Contains(stderr.String(), "Usage:") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestRuntimeAdapter_Claude_cache_hit_does_not_read_auth_body_or_call_HTTP(t *testing.T) {
	now := fixedCLINow()
	fixture := claudeRuntimeAdapter(t)
	adapter := fixture.adapter
	store := cache.New(filepath.Join(t.TempDir(), "cache"), cache.Options{Now: func() time.Time { return now }})
	adapter.newStore = func() (*cache.Store, error) { return store, nil }
	request := evidence.Request{Provider: "claude", Profile: "default"}
	first, err := adapter.ObserveWithCache(t.Context(), request, cache.Policy{Mode: cache.ModeAuto, MaxAge: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(fixture.authFile)
	if err != nil {
		t.Fatal(err)
	}
	malformed := []byte(strings.Repeat("{", int(info.Size())))
	if err := os.WriteFile(fixture.authFile, malformed, info.Mode()); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(fixture.authFile, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	second, err := adapter.ObserveWithCache(t.Context(), request, cache.Policy{Mode: cache.ModeAuto, MaxAge: time.Minute})
	if err != nil || !second.FromCache || !second.Observation.ObservedAt.Equal(first.Observation.ObservedAt) {
		t.Fatalf("first=%+v second=%+v err=%v", first, second, err)
	}
}

func TestCompiledClaudeCobraHelper_prints_scalar_zero(t *testing.T) {
	command := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestClaudeCobraProcessHelper$")
	command.Env = append(os.Environ(), "REMAINDER_CLAUDE_CLI_HELPER=1")
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		t.Fatalf("helper: %v; stdout=%q stderr=%q", err, stdout.String(), stderr.String())
	}
	if stdout.String() != "0\n" || stderr.Len() != 0 {
		t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestClaudeCobraProcessHelper(t *testing.T) {
	if os.Getenv("REMAINDER_CLAUDE_CLI_HELPER") != "1" {
		return
	}
	root, err := os.MkdirTemp("", "remainder-claude-cli-*")
	if err != nil {
		os.Exit(90)
	}
	authFile := filepath.Join(root, ".credentials.json")
	if err := os.WriteFile(authFile, []byte(`{"claudeAiOauth":{"accessToken":"synthetic-access","expiresAt":1788973200000}}`), 0o600); err != nil {
		_ = os.RemoveAll(root)
		os.Exit(91)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/profile") {
			_, _ = w.Write([]byte(`{"account":{"uuid":"account-1"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"limits":[{"group":"session","percent":0}]}`))
	}))
	provider := claude.New(claude.Options{AuthFile: authFile, ProfileEndpoint: server.URL + "/profile", UsageEndpoint: server.URL + "/usage", Client: server.Client(), Now: fixedCLINow})
	adapter := runtimeAdapter{codex: codex.Default(), claude: provider, newStore: func() (*cache.Store, error) { return cache.New(filepath.Join(root, "cache"), cache.Options{}), nil }}
	code := executeWithAdapterAt(t.Context(), []string{"value", "--provider", "claude", "--profile", "default", "--window", "five_hour", "--field", "used", "--cache", "off"}, os.Stdout, os.Stderr, "test", fixedCLINow(), adapter)
	server.Close()
	if err := os.RemoveAll(root); err != nil && code == 0 {
		code = 92
	}
	os.Exit(code)
}

type claudeFixture struct {
	adapter  runtimeAdapter
	authFile string
}

func claudeRuntimeAdapter(t *testing.T) claudeFixture {
	t.Helper()
	now := fixedCLINow()
	authFile := filepath.Join(t.TempDir(), ".credentials.json")
	if err := os.WriteFile(authFile, []byte(`{"claudeAiOauth":{"accessToken":"synthetic-access","expiresAt":1788973200000}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/profile":
			_, _ = w.Write([]byte(`{"account":{"uuid":"account-1"}}`))
		case "/usage":
			_, _ = w.Write([]byte(`{"limits":[{"group":"session","percent":0},{"group":"weekly","percent":25}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	provider := claude.New(claude.Options{AuthFile: authFile, ProfileEndpoint: server.URL + "/profile", UsageEndpoint: server.URL + "/usage", Client: server.Client(), Now: func() time.Time { return now }})
	return claudeFixture{authFile: authFile, adapter: runtimeAdapter{codex: codex.Default(), claude: provider, newStore: func() (*cache.Store, error) {
		return cache.New(filepath.Join(t.TempDir(), "cache"), cache.Options{Now: func() time.Time { return now }}), nil
	}}}
}

func fixedCLINow() time.Time { return time.Date(2026, time.September, 9, 16, 0, 0, 0, time.UTC) }
