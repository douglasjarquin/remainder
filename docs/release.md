# Release artifacts

Remainder release candidates are built from a clean source revision with Go 1.27.1, `CGO_ENABLED=0`, `GOPROXY=off`, trimmed paths, and an embedded release version.
The archive includes the linked Claude, Grok, and Cursor source documents, even where a native route remains unverified.
The archive carries the executable, the MIT license, the standalone quota skill, the provider source record, release instructions, dependency attributions with verbatim required license texts, and a machine-readable source manifest.
`SHA256SUMS` verifies archive integrity when it is obtained from a trusted approved release source.

Remainder is licensed under the [MIT License](../LICENSE).
The [release readiness record](release-readiness.md) separates historical Codex and Cursor evidence from the Grok patch and the v0.2.2 build-target addition.
Version v0.2.1 fixes Grok omitted zero values and includes the selected macOS Grok consumer file route alongside Codex and macOS Cursor.
Version v0.2.2 adds a Linux x86_64 (`linux_amd64`) build target to the packaging and verification scripts; no source behavior changed for any provider or platform. It ships a `linux_amd64` archive only — `darwin_arm64` and `linux_arm64` users see no functional difference and can keep using the immutable v0.2.1 assets for those platforms until they are rebuilt at a later version.
Version v0.2.3 adds the opt-in macOS Keychain fallback for Claude credentials and ships a `darwin_arm64` archive only; `linux_amd64` stays at v0.2.2 and `linux_arm64` at v0.2.1, and neither contains the fallback.
The Claude macOS routes are now both natively verified; the Linux Cursor native route remains unverified.
Publishing requires independent final verification and a version-specific verification record.
Use [GitHub Releases](https://github.com/douglasjarquin/remainder/releases) to find approved assets and their verification records.

## Candidate construction

From a clean checkout at the intended release revision, run:

```sh
scripts/package-release.sh v0.2.2 dist
```

The command builds only for the native supported host.
It currently accepts macOS ARM64, Linux ARM64, and Linux x86_64 (`amd64`) hosts, and refuses other hosts rather than labeling a cross-compiled binary as tested.
It emits one `.tar.gz` archive, `SHA256SUMS`, and an `.asset-qa.json` pre-publication report.
The report records the actual source revision, archive digest, runtime-path exclusions, checks that executed, publication blockers, and checks deliberately left to the final release gate.

## Approved standalone installation

After the `v0.2.2` assets and verification record are published, download the Linux x86_64 archive and checksum file into a new temporary directory (macOS ARM64 and Linux ARM64 users should instead follow this same procedure against the `v0.2.1` assets, since no `v0.2.2` archive was rebuilt for those platforms).
Select exactly one checksum entry because the checksum file also lists other platforms.
Extraction runs only after download and verification succeed:

```sh
asset=remainder_v0.2.2_linux_amd64.tar.gz
release=https://github.com/douglasjarquin/remainder/releases/download/v0.2.2
curl --fail --location --output "$asset" "$release/$asset" &&
curl --fail --location --output SHA256SUMS "$release/SHA256SUMS" &&
awk -v name="$asset" '$2 == name { print; count++ } END { if (count != 1) exit 1 }' SHA256SUMS > "$asset.sha256" &&
sha256sum -c "$asset.sha256" &&
tar -xzf "$asset"
```

On macOS ARM64 or Linux ARM64, follow the [v0.2.1 instructions](https://github.com/douglasjarquin/remainder/releases/tag/v0.2.1) instead (`remainder_v0.2.1_darwin_arm64.tar.gz` or `remainder_v0.2.1_linux_arm64.tar.gz`, `shasum -a 256 -c` on macOS).

Run the extracted binary in place first.
Copy it to a user-selected versioned directory only after it passes `--version` and `--help`.
Copy `skills/remainder` to a user-selected Codex skill directory only with permission to create or replace that location.
The archive does not require Go, Cobra, a Cobra generator, Node, Python, jq, Sum, Herdr, Pinchos, or a daemon at runtime.

Keep versions in separate directories, for example `~/.local/opt/remainder/v0.2.2/remainder`, and point a user-owned symlink or explicit command path at the selected version.
Switching that pointer does not terminate an in-flight process.
Keep the existing v0.2.1 (and v0.2.0) executables for rollback and retain each consumer’s existing collector until its migration is separately approved.
The v0.1.0, v0.2.0, and v0.2.1 tags, assets, and checksum files remain immutable.

## Approved mise configuration

Use mise's explicit `github:douglasjarquin/remainder` backend.
The v0.2.2 release's `mise.toml` pins version `0.2.2` for the `linux-x64` platform only (its `darwin_arm64`/`linux_arm64` archives were not rebuilt at this version); macOS ARM64 and Linux ARM64 hosts should keep using the v0.2.1 `mise.toml`.
It strips the archive's single outer directory so the executable is available at the installation root.
From a separate new temporary directory, download and use that configuration:

```sh
curl --fail --location --output mise.toml https://github.com/douglasjarquin/remainder/releases/download/v0.2.2/mise.toml &&
mise trust mise.toml &&
mise install &&
mise exec -- remainder --version
```

Mise performs installation and verification once; ordinary `remainder` invocations do not invoke mise or check for updates.
The release verification record identifies the mise version and platforms actually tested.

The GitHub backend behavior and options are documented in the official [mise GitHub backend documentation](https://mise.jdx.dev/dev-tools/backends/github.html).
