# remainder

<img width="1280" height="640" alt="3Iwlc" src="https://github.com/user-attachments/assets/dda68c4e-e28f-45dd-a9c6-bf5148179c18" />

## What it is

Remainder is a small, one-shot quota CLI written in Go with Cobra for its command layer.

It is positioned as a personal-use tool for the maintainer's local quota evidence workflow.

The source implements read-only Codex, Claude, Grok, Cursor, and Devin CLI providers, plus help, version, unavailable-provider behavior, and typed evidence output.

Version scope and release evidence are in [docs/remainder/releases.md](docs/remainder/releases.md).

## Features

* **Selected providers.** `--provider` and `--profile default` select Codex, Claude, Grok, Cursor, or Devin. Selectors and source limits are in [docs/remainder/providers.md](docs/remainder/providers.md).

* **`--all`.** Reads the fixed default Codex, Claude, Grok, and Devin contexts concurrently, followed by Cursor on Linux and macOS. The platform controls this list. Rules are in [docs/remainder/cli-contract.md](docs/remainder/cli-contract.md).

* **Compact, JSON, TOON, and scalar output.** The default report is deterministic one-line compact output. `--format json`, `--format toon`, and the scalar `value` fields are in [docs/remainder/providers.md](docs/remainder/providers.md).

* **Pace.** A per-window status from the same observation: `ahead`, `behind`, or `on_pace`. The calculation is in [docs/remainder/cache-and-pace.md](docs/remainder/cache-and-pace.md).

* **Observation cache.** The default `--cache auto --max-age 5s` policy reuses an eligible complete provider observation. Freshness and cache flags are in [docs/remainder/cache-and-pace.md](docs/remainder/cache-and-pace.md).

* **Exit codes.** Exit 0, 1, 2, 3, and 130. Definitions are in [docs/remainder/cli-contract.md](docs/remainder/cli-contract.md).

## Quick Start

### Requirements

Use Go 1.27.1, which is pinned in `mise.toml` and CI, when building from source.

The install script detects `darwin_arm64`, `linux_arm64`, and `linux_amd64` hosts.

### Install

Install the newest release archive for your platform with:

```sh
curl -fsSL https://raw.githubusercontent.com/douglasjarquin/remainder/main/scripts/install.sh | bash
```

The script resolves the newest GitHub release that ships an archive for the detected platform, verifies it against that release's `SHA256SUMS`, and installs the binary into `~/.local/bin`.

`REMAINDER_VERSION`, `REMAINDER_INSTALL_DIR`, Homebrew, the local build, and verification are in [docs/remainder/build-and-verify.md](docs/remainder/build-and-verify.md).

### First report

```sh
remainder --provider codex --profile default
```

An invocation without a provider selection or `--all` writes an honest unavailable message to stderr and exits nonzero.

## How it works

```
you
 │  --provider NAME --profile default, or --all
 ▼
observation cache
 │
 └─ compact, JSON, TOON, or one scalar
```

It does not log in, refresh credentials, switch accounts, invoke another CLI, or make a generative request.

Exit 0 means the selected evidence is usable, including zero, exhausted, and unlimited values.

Exit 1 means the observation is unavailable, exit 2 means invocation or selection is invalid, exit 3 means a partial observation was rendered, and exit 130 means interruption.

Flags, `--all`, and the undefined-pace error are in [docs/remainder/cli-contract.md](docs/remainder/cli-contract.md).

## Documentation

* [docs/remainder/providers.md](docs/remainder/providers.md) — provider selection and output formats.

* [docs/remainder/cli-contract.md](docs/remainder/cli-contract.md) — help, version, flags, `--all`, and exit codes.

* [docs/remainder/cache-and-pace.md](docs/remainder/cache-and-pace.md) — freshness, cache policy, and pace.

* [docs/remainder/build-and-verify.md](docs/remainder/build-and-verify.md) — install details, build, tests, and benchmarks.

* [docs/remainder/releases.md](docs/remainder/releases.md) — version scope and release evidence.

* [docs/remainder/ownership.md](docs/remainder/ownership.md) — roadmap ownership and independent consumers.

* [docs/release.md](docs/release.md) — release artifacts, candidate construction, and the manual procedure.

* [docs/provider-sources.md](docs/provider-sources.md) — source matrix.

* [docs/features/README.md](docs/features/README.md) — feature map.

* [docs/features/claude.md](docs/features/claude.md), [docs/features/grok.md](docs/features/grok.md), [docs/features/cursor.md](docs/features/cursor.md), and [docs/features/devin.md](docs/features/devin.md) — provider collection.

* [docs/release-readiness.md](docs/release-readiness.md) — cross-process, correctness, and performance gates.

* [VERIFY.md](VERIFY.md) — verification.

* [ATTRIBUTIONS.md](ATTRIBUTIONS.md) — attributions.

## Contributing

Human-authored pull requests target `main`. The workflow and checks are in [CONTRIBUTING.md](CONTRIBUTING.md).

## License

Remainder is licensed under the [MIT License](LICENSE).

Pinchos and Sum remain independent consumers.
