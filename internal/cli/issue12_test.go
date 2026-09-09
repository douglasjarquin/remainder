package cli

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/douglasjarquin/remainder/internal/benchmark"
	"github.com/douglasjarquin/remainder/internal/cache"
	"github.com/douglasjarquin/remainder/internal/evidence"
)

type skillFixtureAdapter struct {
	observation evidence.Observation
}

func (a skillFixtureAdapter) Observe(context.Context, evidence.Request) (evidence.Observation, error) {
	return a.observation, nil
}

func (a skillFixtureAdapter) ObserveWithCache(context.Context, evidence.Request, cache.Policy) (cache.Result, error) {
	return cache.Result{Observation: a.observation}, nil
}

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
	binDir := t.TempDir()
	wrapper := filepath.Join(binDir, "remainder")
	if err := os.WriteFile(wrapper, []byte("#!/bin/sh\nexec \"$REMAINDER_ISSUE12_TEST_BINARY\" -test.run=^TestIssue12FixtureProcess$ -- \"$@\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("/bin/sh", "-eu")
	command.Dir = repositoryRoot
	command.Stdin = strings.NewReader(script)
	command.Env = append(os.Environ(),
		"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"REMAINDER_ISSUE12_FIXTURE=1",
		"REMAINDER_ISSUE12_TEST_BINARY="+os.Args[0],
	)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr

	// When
	err = command.Run()

	// Then
	if err != nil || stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("examples: err=%v stdout=%q stderr=%q", err, stdout.String(), stderr.String())
	}
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
	observation := benchmark.Fixtures()[4].Observation
	observation.Profile = "default"
	adapter := skillFixtureAdapter{observation: observation}
	code := ExecuteWithAdapterAt(t.Context(), os.Args[separator:], os.Stdout, os.Stderr, "v0.1.0", benchmark.FixtureTime(), adapter)
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

var (
	_ Adapter      = skillFixtureAdapter{}
	_ cacheAdapter = skillFixtureAdapter{}
)
