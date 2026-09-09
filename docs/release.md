# Release artifacts

Remainder release candidates are built from a clean source revision with Go 1.27.1, `CGO_ENABLED=0`, `GOPROXY=off`, trimmed paths, and an embedded release version.
The archive carries the executable, the MIT license, the standalone Codex skill, the provider source record, release instructions, dependency attributions with verbatim required license texts, and a machine-readable source manifest.
`SHA256SUMS` verifies archive integrity when it is obtained from a trusted approved release source.

Remainder is licensed under the [MIT License](../LICENSE).
This local candidate is not a public release.
The issue 13 Codex readiness result is integrated.
Publication remains pending independent final release verification against that readiness result.

## Candidate construction

From a clean checkout at the intended release revision, run:

```sh
scripts/package-release.sh v0.1.0 dist
```

The command builds only for the native supported host.
It currently accepts macOS ARM64 and Linux ARM64 hosts, and refuses other hosts rather than labeling a cross-compiled binary as tested.
It emits one `.tar.gz` archive, `SHA256SUMS`, and an `.asset-qa.json` pre-publication report.
The report records the actual source revision, archive digest, runtime-path exclusions, checks that executed, publication blockers, and checks deliberately left to the final release gate.

## Approved standalone installation

Download an approved release archive and its `SHA256SUMS` file from that release's immutable asset URLs into a temporary directory.
Verify the checksum there before extracting:

```sh
shasum -a 256 -c SHA256SUMS
tar -xzf remainder_v0.1.0_darwin_arm64.tar.gz
```

Run the extracted binary in place first.
Copy it to a user-selected versioned directory only after it passes `--version` and `--help`.
Copy `skills/remainder` to a user-selected Codex skill directory only with permission to create or replace that location.
The archive does not require Go, Cobra, a Cobra generator, Node, Python, jq, Sum, Herdr, Pinchos, or a daemon at runtime.

Keep versions in separate directories, for example `~/.local/opt/remainder/v0.1.0/remainder`, and point a user-owned symlink or explicit command path at the selected version.
Switching that pointer does not terminate an in-flight process.
No earlier Remainder version exists for first-release rollback; retain the existing `quota-axi --provider codex --no-credential-refresh` command until the consumer migration is separately approved.

## Approved mise configuration

The installed mise 2026.9.3 has no `remainder` registry shorthand.
For an approved GitHub release, mise's GitHub backend can use the explicit `github:douglasjarquin/remainder` tool identifier pinned to `v0.1.0`, with an asset pattern selecting the current operating-system and architecture archive and checksum verification configured against the published `SHA256SUMS` asset.
Create that configuration only after substituting the approved release asset names and immutable release URLs.
This local candidate does not claim a mise GitHub installation.

The GitHub backend behavior and options are documented in the official [mise GitHub backend documentation](https://mise.jdx.dev/dev-tools/backends/github.html).
