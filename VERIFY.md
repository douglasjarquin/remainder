# Verification

The portable verification entrypoint is `mise run verify`.

This repository is a one-shot Go CLI with no long-lived service or external runtime state.

```verify
entrypoint = "mise run verify"
feature_maps = "docs/features/README.md"
artifacts = ".artifacts/verification"
evidence = ".artifacts/evidence"
task_owner = "."
timeout_seconds = 600

[requires]
commands = ["git", "mise", "python3", "go"]

[freshness]
inputs = []
outputs = []
```

## Setup

Use Go 1.27.1 selected by `mise.toml`.

Before offline verification in a fresh checkout or empty module cache, prime the exact versions already recorded in `go.mod` and `go.sum` once with `GOTOOLCHAIN=local go mod download all`.

The setup command is the only network-enabled dependency step.

## Readiness

Run `python3 .agents/skills/verify/scripts/verify_run.py --check` to validate this contract, its feature maps, task ownership, and required commands without running the application.

Run `mise run build` to produce the CGO-free binary before driving the CLI scenarios.

The CLI has no readiness endpoint and starts no service.

## Automated checks

The canonical project check is `mise run verify`.

It runs formatting, `GOPROXY=off go vet ./...`, `GOPROXY=off go test -race -shuffle=on -count=1 ./...`, `GOPROXY=off ./scripts/check-dependencies.sh`, and a CGO-free release-like build.

The direct checks are `GOPROXY=off go vet ./...`, `GOPROXY=off go test -race -shuffle=on -count=1 ./...`, `GOPROXY=off ./scripts/check-dependencies.sh`, and `CGO_ENABLED=0 GOPROXY=off go build -trimpath -ldflags='-s -w -X main.version=v0.1.0' -o /tmp/remainder ./cmd/remainder`.

Run `mise run benchmark` for the retained process JSONL and in-process allocation evidence.
Controlled refresh uses a compiled test helper with synthetic auth and loopback TLS, independently counts one request per Codex or Grok sample and two per Claude sample and three per Cursor sample, and separates helper-process timing from summed request timing to response headers.
Use `scripts/benchmark.sh bin/remainder --provider claude` or `scripts/benchmark.sh bin/remainder --provider grok` to select provider refresh and cache measurements.
Use `scripts/benchmark.sh bin/remainder --provider cursor` for Cursor on Linux or macOS.
The macOS controlled helper injects a synthetic Keychain reader, while cache-hit measurements drive the actual binary without Keychain permission.
Linux timings remain trend evidence rather than certification of the Darwin ARM64 cache objective.
Neither timing certifies release-binary refresh or live provider latency.
Eligible cache-hit timing uses the actual release-like binary with a seeded synthetic observation, zero provider requests, and a separately counted JSON provenance check.

The issue #7 release-process acceptance workload is an explicit developer command rather than a canonical verification dependency.
Build untagged CGO-free Linux ARM64 `cmd/remainder` and `cmd/remainder-benchmark` binaries, mount them read-only into the pinned Ubuntu 24.04 ARM64 image, and run the benchmark binary with `--binary /probe/remainder --coalescing-acceptance` under `--network none`, a writable `/tmp` tmpfs, and `--rm`.
The runner creates only synthetic auth, an ephemeral CA with the `chatgpt.com` DNS SAN, a loopback TLS endpoint, and an allowlisted CONNECT proxy inside that owned container.
It fails unless Sum/Pinchos applications are absent and the exact process, request, projection, account, observation, latency, and memory assertions pass.

## Scenarios

The linked [feature map](docs/features/README.md) contains the automated Cobra and renderer scenarios.

The final CLI checks drive `bin/remainder --help`, `bin/remainder --version`, `bin/remainder --freshness ignored`, `bin/remainder`, and the `value` command through isolated process invocations.

Canonical scenarios use only synthetic credentials, temporary cache roots, and local test servers; they do not access real credentials, user caches, or the external network.
Codex, Claude, Grok, and Cursor integration tests use synthetic temporary auth files, injected Keychain readers, bounded synthetic helper processes, and loopback `httptest` servers only.

## Isolation

Each check runs in this checkout and uses only temporary build output or the Git-ignored `.artifacts/verification` directory.

The CLI is one-shot and binds no port.

No real user profile, credential store, daemon, database, container, or shared service is used.

## Artifacts

The verification runner writes run records under `.artifacts/verification/<run-id>/run.json` and its command log.

Manual CLI evidence is kept under `.artifacts/evidence` or the task-local evidence directory referenced by the handoff.

Run records and evidence are Git-ignored runtime artifacts and must remain available until review is complete.

## Teardown

No service teardown is required because every scenario is a one-shot process.

Remove only task-local temporary processes or files created by a manual scenario after its evidence is captured.

Do not remove the verification run directory or its evidence during teardown.

## Policy

Changes to this contract, `mise.toml`, `.agents/skills/verify/`, `.agents/skills/evidence/`, `.agents/skills/maintain-verification/`, or the feature maps require independent root review.

This policy covers Codex, Claude, Grok, and Cursor CLI collection and cache behavior with synthetic local sources and temporary cache roots.
It does not authorize a live provider canary or any runtime dependency beyond the standard library and pinned Cobra graph.

It checks formatting, `go vet`, race-enabled shuffled tests, the reviewed Cobra dependency/import contract, and a CGO-free release-like build.

Before running offline verification from a fresh checkout or empty module cache, prime the exact versions already recorded in `go.mod` and `go.sum` once with the network-enabled setup command:

```sh
GOTOOLCHAIN=local go mod download all
```

Use the Go toolchain selected by the existing `mise.toml` and CI pin for this setup step.
After setup, the verification commands below require the populated module cache and keep `GOPROXY=off`.

The direct Go commands are:

```sh
GOPROXY=off go vet ./...
GOPROXY=off go test -race -shuffle=on -count=1 ./...
GOPROXY=off ./scripts/check-dependencies.sh
CGO_ENABLED=0 GOPROXY=off go build -trimpath -ldflags='-s -w -X main.version=v0.1.0' -o /tmp/remainder ./cmd/remainder
```

The verification command does not access real credentials, an external provider, a user cache, or the external network.

The task runner equivalent is `mise run verify`.
