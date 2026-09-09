package cli

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/douglasjarquin/remainder/internal/cache"
	"github.com/douglasjarquin/remainder/internal/codex"
	"github.com/douglasjarquin/remainder/internal/evidence"
)

func TestSkillFrontmatter_declaresNativeRemainderIdentity(t *testing.T) {
	// Given
	skill, err := os.ReadFile(filepath.Join("..", "..", "skills", "remainder", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	sections := strings.SplitN(string(skill), "---", 3)
	if len(sections) != 3 {
		t.Fatal("skill frontmatter is missing")
	}
	fields := make(map[string]string)
	for line := range strings.SplitSeq(sections[1], "\n") {
		key, value, ok := strings.Cut(line, ":")
		if ok {
			fields[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}

	// When
	name, description := fields["name"], fields["description"]

	// Then
	if name != "remainder" || description == "" {
		t.Fatalf("frontmatter identity: name=%q description=%q", name, description)
	}
}

func TestSkillExamples_runThroughCompiledCobraFixture(t *testing.T) {
	// Given
	repositoryRoot := filepath.Join("..", "..")
	examples, err := os.ReadFile(filepath.Join(repositoryRoot, "skills", "remainder", "references", "examples.md"))
	if err != nil {
		t.Fatal(err)
	}
	script := shellExamples(string(examples))
	if strings.TrimSpace(script) == "" {
		t.Fatal("no shell examples found")
	}
	var requests atomic.Int64
	server := newSkillFixtureServer(t, &requests)

	// When
	stdout, stderr, err := runSkillScript(t, repositoryRoot, script, server.URL)

	// Then
	if err != nil || stderr != "" {
		t.Fatalf("examples: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "Usage:\n  remainder [flags]") || !strings.Contains(stdout, "Usage:\n  remainder value [flags]") {
		t.Fatalf("help was not visible: stdout=%q", stdout)
	}
	compactCount, jsonCount, remainingCount, paceCount := 0, 0, 0, 0
	for line := range strings.SplitSeq(stdout, "\n") {
		switch {
		case strings.HasPrefix(line, "schema=v1 "):
			compactCount++
			if !strings.Contains(line, `source="native_file_http/codex_auth_json"`) || !strings.Contains(line, "identity=verified") || !strings.Contains(line, "remaining=42percent") || !strings.Contains(line, "pace=ahead") {
				t.Fatalf("compact provenance: %q", line)
			}
		case strings.HasPrefix(line, "{"):
			jsonCount++
			observation, parseErr := evidence.ParseJSON([]byte(line))
			if parseErr != nil || observation.Source.Kind != "native_file_http" || observation.Source.Name != "codex_auth_json" || observation.Account.Binding != evidence.IdentityHistorical || !observation.ObservedAt.Equal(time.Date(2026, time.March, 8, 7, 30, 0, 0, time.UTC)) {
				t.Fatalf("JSON provenance: observation=%+v error=%v", observation, parseErr)
			}
			var weekly *evidence.Window
			for index := range observation.Windows {
				if observation.Windows[index].ID == "weekly" {
					weekly = &observation.Windows[index]
					break
				}
			}
			if weekly == nil || weekly.Pace == nil || weekly.Pace.Status != evidence.PaceAhead {
				t.Fatalf("JSON weekly pace: %+v", weekly)
			}
		case line == "42":
			remainingCount++
		case line == "ahead":
			paceCount++
		}
	}
	if compactCount != 1 || jsonCount != 1 || remainingCount != 2 || paceCount != 1 || requests.Load() != 1 {
		t.Fatalf("outputs: compact=%d json=%d remaining=%d pace=%d requests=%d", compactCount, jsonCount, remainingCount, paceCount, requests.Load())
	}
	t.Logf("compiled helper: compact=%d json=%d remaining=%d pace=%d provider_requests=%d", compactCount, jsonCount, remainingCount, paceCount, requests.Load())
}

func TestSkillPinchosExample_runsAloneOnFirstPoll(t *testing.T) {
	// Given
	repositoryRoot := filepath.Join("..", "..")
	examples, err := os.ReadFile(filepath.Join(repositoryRoot, "skills", "remainder", "references", "examples.md"))
	if err != nil {
		t.Fatal(err)
	}
	script := pinchosExample(shellExamples(string(examples)))
	if strings.TrimSpace(script) == "" {
		t.Fatal("direct Pinchos example not found")
	}
	var requests atomic.Int64
	server := newSkillFixtureServer(t, &requests)

	// When
	stdout, stderr, err := runSkillScript(t, repositoryRoot, script, server.URL)

	// Then
	if err != nil || stdout != "42\n42\n" || stderr != "" || requests.Load() != 1 {
		t.Fatalf("direct Pinchos example: err=%v stdout=%q stderr=%q requests=%d", err, stdout, stderr, requests.Load())
	}
	t.Logf("direct Pinchos first poll: stdout=%q provider_requests=%d", strings.TrimSpace(stdout), requests.Load())
}

func newSkillFixtureServer(t *testing.T, requests *atomic.Int64) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		if request.Method != http.MethodGet || request.Header.Get("Authorization") != "Bearer synthetic-secret" || request.Header.Get("ChatGPT-Account-Id") != "acct-test" {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		fmt.Fprint(w, `{"account_id":"acct-test","rate_limit":{"primary_window":{"used_percent":58,"limit_window_seconds":604800,"reset_after_seconds":604800}}}`)
	}))
	t.Cleanup(server.Close)
	return server
}

func runSkillScript(t *testing.T, repositoryRoot, script, endpoint string) (string, string, error) {
	t.Helper()
	syntheticHome := t.TempDir()
	authPath := filepath.Join(syntheticHome, ".codex", "auth.json")
	if err := os.MkdirAll(filepath.Dir(authPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(authPath, []byte(`{"tokens":{"access_token":"synthetic-secret","account_id":"acct-test"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	binDir := filepath.Join(syntheticHome, "bin")
	if err := os.Mkdir(binDir, 0o700); err != nil {
		t.Fatal(err)
	}
	wrapper := filepath.Join(binDir, "remainder")
	if err := os.WriteFile(wrapper, []byte("#!/bin/sh\nexec \"$REMAINDER_ISSUE12_TEST_BINARY\" -test.run=^TestIssue12FixtureProcess$ -- \"$@\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("/bin/sh", "-eu")
	command.Dir = repositoryRoot
	command.Stdin = strings.NewReader(script)
	command.Env = []string{
		"PATH=" + binDir + string(os.PathListSeparator) + os.Getenv("PATH"),
		"HOME=" + syntheticHome,
		"CODEX_HOME=" + filepath.Join(syntheticHome, ".codex"),
		"XDG_CACHE_HOME=" + filepath.Join(syntheticHome, ".cache"),
		"REMAINDER_ISSUE12_FIXTURE=1",
		"REMAINDER_ISSUE12_TEST_BINARY=" + os.Args[0],
		"REMAINDER_ISSUE12_ENDPOINT=" + endpoint,
		"REMAINDER_ISSUE12_AUTH=" + authPath,
		"REMAINDER_ISSUE12_CACHE=" + filepath.Join(syntheticHome, ".cache", "remainder", "v1"),
	}
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	return stdout.String(), stderr.String(), err
}

func TestIssue12FixtureProcess(t *testing.T) {
	if os.Getenv("REMAINDER_ISSUE12_FIXTURE") != "1" {
		return
	}
	separator := 0
	for index, arg := range os.Args {
		if arg == "--" {
			separator = index + 1
			break
		}
	}
	now := time.Date(2026, time.March, 8, 7, 30, 0, 0, time.UTC)
	provider := codex.New(codex.Options{
		AuthFile:  os.Getenv("REMAINDER_ISSUE12_AUTH"),
		Endpoints: []string{os.Getenv("REMAINDER_ISSUE12_ENDPOINT")},
		Timeout:   time.Second,
		Now:       func() time.Time { return now },
	})
	store := cache.New(os.Getenv("REMAINDER_ISSUE12_CACHE"), cache.Options{Now: func() time.Time { return now }})
	adapter := runtimeAdapter{codex: provider, newStore: func() (*cache.Store, error) { return store, nil }}
	code := executeWithAdapterAt(t.Context(), os.Args[separator:], os.Stdout, os.Stderr, "v0.1.0", now, adapter)
	os.Exit(code)
}

func shellExamples(markdown string) string {
	var script strings.Builder
	inShellBlock := false
	for line := range strings.SplitSeq(markdown, "\n") {
		switch {
		case line == "```sh":
			inShellBlock = true
		case line == "```" && inShellBlock:
			inShellBlock = false
		case inShellBlock:
			script.WriteString(line)
			script.WriteByte('\n')
		}
	}
	return script.String()
}

func pinchosExample(script string) string {
	const function = "pinchos_read_codex_weekly_remaining() {"
	start := strings.Index(script, function)
	if start < 0 {
		return ""
	}
	direct := strings.TrimSpace(script[start:])
	_, invocation, ok := strings.CutLast(direct, "\n")
	if !ok || strings.TrimSpace(invocation) == "" {
		return ""
	}
	return direct + "\n" + invocation + "\n"
}
