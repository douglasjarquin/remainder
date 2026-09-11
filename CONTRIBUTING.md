# Contributing

Thanks for wanting to contribute to Remainder. This is a small, one-shot Go quota CLI for the maintainer's local quota evidence workflow, and focused changes that keep that contract intact are welcome.

## Workflow

Human-authored pull requests target `main`. Fork the repository, create a branch from current `main`, make your change, then open a pull request against `main`.

Ordinary GitHub fork, branch, and pull-request flow is enough. There is no extra contribution tool.

1. Fork [douglasjarquin/remainder](https://github.com/douglasjarquin/remainder).
2. Clone your fork and keep `main` current with the upstream default branch.
3. Create a topic branch from `main`.
4. Commit a focused change.
5. Run the checks in [VERIFY.md](VERIFY.md).
6. Open a pull request targeting `main`.

## Verification

[VERIFY.md](VERIFY.md) is the source of truth for how this repository is checked. Follow that contract rather than adding verification policy here.

Canonical entrypoints already documented in `VERIFY.md` and [AGENTS.md](AGENTS.md):

- `mise run verify` is the portable project check (`scripts/verify.sh`).
- Use Go 1.27.1, selected by `mise.toml` and CI.
- Before offline verification in a fresh checkout or empty module cache, prime the pinned module graph once with `GOTOOLCHAIN=local go mod download all`.
- `mise run test` runs race-enabled shuffled tests. The direct equivalent is `go test -race -shuffle=on -count=1 ./...`.
- `mise run build` produces the CGO-free `bin/remainder` with `CGO_ENABLED=0`.

After that one-time module download, verification itself keeps module lookup disabled (`GOPROXY=off`). The full command list, formatting, `go vet`, dependency/import check, CGO-free release-like build, isolation rules, and scenarios live in `VERIFY.md`. CI on `main` and pull requests uses the same Go 1.27.1 pin on Ubuntu 24.04 and macOS 14.

## Repo conventions

Remainder is a Go quota CLI with a standard-library core and a Cobra command layer. It is a personal-use maintainer tool, not a hosted service.

- Runtime and ordinary tests use the Go standard library plus the pinned Cobra graph.
- Release-like builds use `CGO_ENABLED=0`.
- The CLI is one-shot: it binds no port and starts no service.
- Help, version, validation, and ordinary offline tests perform no credential, cache, network, telemetry, or provider access.
- Future provider commands must preserve the one-shot CLI contract and honest unavailable behavior.
- `cmd/remainder/main.go` owns OS signal and process-stream wiring. `internal/cli/run.go` owns flag parsing, output, and exit semantics.
- Keep changes aligned with [README.md](README.md), [AGENTS.md](AGENTS.md), and the [feature map](docs/features/README.md).
- Match the formatting, dependency, and test checks already enforced by `mise run verify` and `.github/workflows/ci.yml`.

Do not invent extra product policy, credential-store behavior, or network access on ordinary offline paths.

## Questions

Open a GitHub issue on [douglasjarquin/remainder](https://github.com/douglasjarquin/remainder/issues). There is no Discord or other external chat required.
