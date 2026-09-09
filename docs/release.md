# Release artifacts

Remainder release candidates are built from a clean source revision with Go 1.27.1, `CGO_ENABLED=0`, `GOPROXY=off`, trimmed paths, and an embedded release version.
The archive includes the linked Claude, Grok, and Cursor source documents, even where a native route remains unverified.
The archive carries the executable, the MIT license, the standalone quota skill, the provider source record, release instructions, dependency attributions with verbatim required license texts, and a machine-readable source manifest.
`SHA256SUMS` verifies archive integrity when it is obtained from a trusted approved release source.

Remainder is licensed under the [MIT License](../LICENSE).
The [release readiness record](release-readiness.md) separates the historical Codex evidence from the macOS Cursor increment.
Version v0.2.0 adds the macOS Cursor CLI Keychain route; Claude, Grok, and Linux Cursor native routes are not certified by this release.
Publishing requires independent final verification and a version-specific verification record.
Use [GitHub Releases](https://github.com/douglasjarquin/remainder/releases) to find approved assets and their verification records.

## Candidate construction

From a clean checkout at the intended release revision, run:

```sh
scripts/package-release.sh v0.2.0 dist
```

The command builds only for the native supported host.
It currently accepts macOS ARM64 and Linux ARM64 hosts, and refuses other hosts rather than labeling a cross-compiled binary as tested.
It emits one `.tar.gz` archive, `SHA256SUMS`, and an `.asset-qa.json` pre-publication report.
The report records the actual source revision, archive digest, runtime-path exclusions, checks that executed, publication blockers, and checks deliberately left to the final release gate.

## Approved standalone installation

After the `v0.2.0` assets and verification record are published, download the macOS ARM64 archive and checksum file into a new temporary directory.
Select exactly one checksum entry because the checksum file also lists other platforms.
Extraction runs only after download and verification succeed:

```sh
asset=remainder_v0.2.0_darwin_arm64.tar.gz
release=https://github.com/douglasjarquin/remainder/releases/download/v0.2.0
curl --fail --location --output "$asset" "$release/$asset" &&
curl --fail --location --output SHA256SUMS "$release/SHA256SUMS" &&
awk -v name="$asset" '$2 == name { print; count++ } END { if (count != 1) exit 1 }' SHA256SUMS > "$asset.sha256" &&
shasum -a 256 -c "$asset.sha256" &&
tar -xzf "$asset"
```

On Linux ARM64, select `remainder_v0.2.0_linux_arm64.tar.gz` and use `sha256sum -c "$asset.sha256"` for the checksum command.

Run the extracted binary in place first.
Copy it to a user-selected versioned directory only after it passes `--version` and `--help`.
Copy `skills/remainder` to a user-selected Codex skill directory only with permission to create or replace that location.
The archive does not require Go, Cobra, a Cobra generator, Node, Python, jq, Sum, Herdr, Pinchos, or a daemon at runtime.

Keep versions in separate directories, for example `~/.local/opt/remainder/v0.2.0/remainder`, and point a user-owned symlink or explicit command path at the selected version.
Switching that pointer does not terminate an in-flight process.
Keep the existing v0.1.0 executable for Codex rollback and retain each consumer’s existing collector until its migration is separately approved.
The v0.1.0 tag, assets, and checksum file are immutable.

## Approved mise configuration

Use mise's explicit `github:douglasjarquin/remainder` backend.
The release's `mise.toml` pins version `0.2.0`, the exact platform archive names, and each archive's SHA-256 digest.
It strips the archive's single outer directory so the executable is available at the installation root.
From a separate new temporary directory, download and use that configuration:

```sh
curl --fail --location --output mise.toml https://github.com/douglasjarquin/remainder/releases/download/v0.2.0/mise.toml &&
mise trust mise.toml &&
mise install &&
mise exec -- remainder --version
```

Mise performs installation and verification once; ordinary `remainder` invocations do not invoke mise or check for updates.
The release verification record identifies the mise version and platforms actually tested.

The GitHub backend behavior and options are documented in the official [mise GitHub backend documentation](https://mise.jdx.dev/dev-tools/backends/github.html).
