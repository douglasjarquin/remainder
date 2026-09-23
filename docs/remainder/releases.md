# Release notes

Version scope and release evidence. Moved out of the README.

The v0.1.0 artifacts support Codex.
The v0.2.1 release scope includes Codex, the verified macOS Cursor CLI Keychain route, and the selected macOS Grok consumer file route.
The patch fixes omitted Grok zero values without combining shared allowance, product limits, and prepaid credits.
v0.2.2 adds a `linux_amd64` build target with no provider or output change; v0.2.3 adds the opt-in macOS Keychain fallback for Claude credentials and ships `darwin_arm64` only.
v0.3.0 adds `--format toon` and ships `darwin_arm64` with the executable at `bin/remainder` inside the archive.
v0.4.0 adds the Devin CLI provider (`--provider devin` and a fifth `--all` entry) and ships `darwin_arm64` only; the archive keeps the `bin/remainder` layout.
See [release notes](docs/release.md).
The Linux Cursor native route remains unverified; retain existing collectors for it.
Use the published release verification record and the [source matrix](docs/provider-sources.md) to distinguish native certification from controlled fixtures.

The selected native Codex macOS route passed two authorized read-only observations on 2026-09-09, with matching verified account bindings and unchanged credential file metadata; see the [source evidence](docs/provider-sources.md).
Controlled HTTP/TLS and temp-home tests cover provider and cache failure cases, and the [release-readiness record](docs/release-readiness.md) consolidates the completed cross-process, correctness, and performance gates.
The [published releases](https://github.com/douglasjarquin/remainder/releases) carry version-specific executable checksums and verification records.
A native source canary does not by itself certify a packaged or downloaded executable.

Release candidate construction, the strict asset contents, checksum verification, and future standalone installation are documented in [the release guide](docs/release.md).
New provider increments require their own source-specific release evidence before publication.
