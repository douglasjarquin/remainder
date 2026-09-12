# Release readiness

## v0.3.0 scope

This release adds `--format toon` (source merge `a4f283b`, PR 47 / issue 46) so a caller can render the same paced observation JSON already emits as a TOON document.
Compact stays the default; JSON and `value` stay unchanged.
It is also the first published archive that places the executable at `bin/remainder` (packaging change `a48b9dd`), so a GitHub-release installer such as mise's `github:` backend does not treat `docs/` and `skills/` as binaries.
Verified on this macOS arm64 build host: `gofmt -l .` clean, `go vet ./...`, `go test -race -shuffle=on -count=1 ./...` (all packages pass), `./scripts/check-dependencies.sh`, and the evidence redaction suite, all via `./scripts/verify.sh`.
`scripts/package-release.sh v0.3.0 dist` is the publication packaging command for the native `darwin_arm64` archive.
This release ships a `darwin_arm64` archive only; `linux_amd64` users keep the immutable v0.2.2 asset and `linux_arm64` users keep v0.2.1, neither of which contains `--format toon` or the `bin/` layout.
No new credential route is introduced, so this increment does not run a live provider canary.
It does not repeat the hosted CI matrix or a separate independent reviewer.

## v0.2.3 scope

This release adds the opt-in macOS Keychain fallback for Claude credentials (source commit `f2430e1f`), so a macOS host whose Claude Code CLI stores its OAuth credentials in the login Keychain rather than `~/.claude/.credentials.json` can be read.
The file route is tried first on every OS and is unchanged; the fallback runs only when that file is absent, the OS is macOS, and `--allow-keychain-prompt` is passed.
Verified on this macOS arm64 build host: `gofmt -l .` clean, `go vet ./...`, `go test -race -shuffle=on -count=1 ./...` (all packages pass), `./scripts/check-dependencies.sh`, and the evidence redaction suite, all via `./scripts/verify.sh`.
`scripts/package-release.sh v0.2.3 dist` produced a `darwin_arm64` archive whose 15 pre-publication asset checks all passed.
On 2026-09-10 the macOS Keychain fallback passed an authorized native canary on a macOS 15 arm64 host, using the packaged `remainder_v0.2.3_darwin_arm64` executable rather than a source build.
With the selected credential file absent, the read returned exit 0, fresh complete evidence, source kind `native_keychain_http` name `claude_keychain`, and a verified account binding.
It preserved the account five-hour and weekly windows, the Fable model weekly window, and distinct paid `extra_usage` credits.
A second read inside the max-age window reused the same observation timestamp, and the same request without `--allow-keychain-prompt` on a cold cache returned unavailable naming the flag with exit 1.
No login, credential refresh, account switch, or generative request occurred.
One host and one account were exercised; other macOS versions, accounts, and the Linux Cursor route remain unverified.
Codex and Grok scalar reads were re-run on the packaged executable and were unchanged.
This release ships a `darwin_arm64` archive only; `linux_amd64` users keep the immutable v0.2.2 asset and `linux_arm64` users keep v0.2.1, neither of which contains the Keychain fallback.
It does not repeat the hosted CI matrix or a separate independent reviewer.

## v0.2.2 scope

This patch adds a `linux_amd64` (Linux x86_64) build target to `scripts/package-release.sh` and `scripts/verify-release-asset.sh`, and generalizes `scripts/package-release.test.sh`'s host-detection switch (previously hardcoded to treat `arm64` as the only native arch) to a `native_arch`/`foreign_arch` pair per host. No provider source, cache, output, or CLI behavior changed; `internal/` and `cmd/` are untouched since v0.2.1 aside from the already-released Claude native-canary documentation update.
Verified on this linux/amd64 build host only: `go build ./...`, `go vet ./...`, `go test -race -shuffle=on -count=1 ./...` (all packages pass), and `sh ./scripts/package-release.test.sh` (packages, checksums, and verifies a real `remainder_v0.2.2_linux_amd64.tar.gz`).
This release does not repeat the full historical readiness ceremony (hosted macOS+Ubuntu CI matrix, a separate independent reviewer, a native provider canary) — it is a build-tooling addition, not a provider or output change, and no darwin/arm64 host was available to rebuild those platforms. `darwin_arm64` and `linux_arm64` remain pinned at their v0.2.1 assets, which are unaffected by this patch.

## v0.2.1 scope

This patch adds the selected macOS Grok consumer file route to the Codex and macOS Cursor release scope.
Source commit `31941c9adb668709467586b34fef83c06a8f7805` fixes omitted protobuf scalars: a valid quota cycle permits zero used and 100 percent remaining, and a present empty prepaid message means zero credits.
Malformed known fields encoded as groups remain errors; absent entitlements are not invented.
Six authorized native CLI commands passed with shared remaining 100 percent, prepaid zero, and behind pace, while cached projections retained the original observation timestamp and unknown account identity.
Credential metadata remained unchanged and the isolated cache was removed; no login, credential refresh, or generative request occurred.
The final source passed 42 automated checks, independent review, macOS and Ubuntu CI, and a 100-sample Grok cache benchmark with p95 8.25625 ms against the unchanged 10 ms target.
Final v0.2.1 packaged and public-download checks are recorded separately in the release verification.json; this source evidence alone does not replace them.
Claude and Linux Cursor native routes remain unverified, and Sum/Herdr integration remains deferred.

## Historical v0.2.0 scope

The release scope is Codex plus the macOS Cursor CLI Keychain route.
Claude, Grok, and Linux Cursor native canaries remain unverified; their source fixtures are not native release certification.
Unsupported desktop SQLite routes and user-deferred Sum/Herdr integration remain outside this release.

Cursor source commit `00471fb37b58294b1410e2044ec089bb24c188b4` passed an authorized headless native read with caching disabled: five fresh windows, unknown account identity, and no stderr.
It passed 42 automated checks, actual Mac and Linux fixture CLI scenarios, independent review, and both hosted CI jobs before PR #33 merged.
The initial Cursor 100-sample cache timing missed the 10 ms p95 objective at 10.311584 ms.
The predefined combined 200 valid samples passed at 9.473292 ms, retaining the original samples and a 28.092833 ms maximum; an earlier comparison-harness failure had no usable timing result and remains inconclusive.
This source evidence does not replace final packaged and downloaded executable verification.
The release’s verification.json must bind those results, source revision, native route, platform, archive digests, and remaining limitations.

## Historical v0.1.0 readiness

The following record describes the first Codex source gate.
The immutable v0.1.0 release subsequently completed its packaged and downloaded executable checks.

### Scope and evidence

The first release supports only the native Codex `default` profile through Remainder's compiled Cobra entrypoint.
Claude, Grok, Cursor, consumer migration, and packaging are outside this readiness decision.

The retained runtime evidence is bound to full source SHA `bd9ca8b9928dfb993a83abb89dde840bbc146ed5`.
Any later test or documentation commit requires the ordinary final-SHA root rerun before merge.
Fixture and source canaries remain separate because synthetic correctness cannot establish a live credential route.

The canonical offline verification passed after the pinned module cache was populated, using Go 1.27.1 and Python 3.13.5.
It covered formatting, vet, race-enabled shuffled tests, dependency policy, evidence helpers, and a stripped CGO-free build.
The source dependency audit found only the pinned Cobra graph recorded in `ATTRIBUTIONS.md`, with no Viper, generator, Node, Python, jq, or quota-axi runtime requirement.
Installation proof that a downloaded executable needs no separately installed Go or Cobra runtime remains the issue #14 gate.

The full-process Apple Silicon cache-hit run recorded 10 samples with p95 8.627084 ms against the fixed 10 ms objective, 61 total benchmark processes, 10 controlled provider requests, and a clean CGO-free binary.
The in-process allocation gates retained the fixed non-pace compact, JSON, and remaining-scalar ceilings of 130, 120, and 125 allocations, plus the distinct pace-percent compact, JSON, and scalar ceilings of 180, 180, and 140 allocations.
The deterministic goldens covered healthy, exhausted, stale, partial-unknown, and shared-percent observations through compact, JSON, and scalar Cobra paths.
The pace fuzz target passed against its seeded reserve classifier.

The pinned `tiktoken` `o200k_base` measurement recorded 804 tokens for the minimum skill plus both help surfaces and 18 tokens for the representative missing-auth failure.
Missing tokenizers for unrelated models do not expand or block the Codex-only claim.
The pinned quota-axi fixture comparison matched the declared weekly percentage subset; it did not establish full-payload equivalence or a speed advantage.

Four temporary seeded mutations were run against the real tests and then reverted.
Weakening account matching failed `TestObservation_SelectValueRequiresExactEligibleSelection` with an unexpected successful selection.
Disabling fresh-only enforcement failed the same test with an unexpected stale number.
Returning zero for an absent requested limit initially survived the suite, so `TestObservation_SelectValueRejectsMissingLimit` was added and failed that mutation before passing against the restored implementation.
Including a malformed response body in an error failed the non-JSON case in `TestAdapterObserve_rejectsInvalidEvidenceSafely` by exposing its synthetic secret marker.

The separately approved native canary at `bd9ca8b9928dfb993a83abb89dde840bbc146ed5` passed all seven documented help, compact, JSON, scalar, and direct consumer commands against the selected Codex/default context.
It preserved one observation across cache reuse, reported the same weekly scalar and pace, left credential metadata unchanged, and performed no login, refresh, account switch, or generative request.
This observation establishes only that selected source, account context, date, and host; it does not establish future provider availability, another account, or another operating system.

The prior issue #7 Linux acceptance used 12 ordinary release processes per concurrency workload and retained request counts, kernel-lock behavior, bounded failure reuse, memory, and container cleanup evidence.
The final candidate still requires the independent root rerun on its exact full SHA.

## Reusable release checklist

- [ ] Freeze a clean candidate and record its full SHA, toolchain versions, platform, and source diff.
- [ ] Prime only the pinned module graph once, then run `mise run verify` with dependency lookup offline.
- [ ] Confirm help, version, invalid input, and unavailable behavior through the compiled binary without auth, cache, or provider access.
- [ ] Drive compact, JSON, remaining scalar, pace scalar, partial success, cancellation, and fresh command instances through the real Cobra tree.
- [ ] Run the wrong-account, stale-number, missing-limit, and secret-leak mutations and require each targeted test command to exit nonzero before restoring the source.
- [ ] Run deterministic goldens, the pace fuzz target, race-enabled shuffled tests, and the aggregate verification gate.
- [ ] Keep the non-pace fixture ceilings at compact 130, JSON 120, and remaining scalar 125, and the distinct pace-percent fixture ceilings at compact 180, JSON 180, and pace scalar 140 unless a recorded investigation approves a change.
- [ ] Measure raw full-process samples through the CGO-free binary and require eligible Apple Silicon cache-hit p95 to remain at or below 10 ms.
- [ ] Record process, request, allocation, memory, output, and cleanup facts without combining local, harness, network, or auth latency.
- [ ] Run the pinned comparator and declared tokenizer only from pre-provisioned isolated environments, with external lookup disabled and uncertainty preserved.
- [ ] Keep explicitly approved native canaries for the release’s certified routes separate from fixtures and record the exact source SHA, selection, auth behavior, metadata stability, and cleanup.
- [ ] Re-run supported macOS and Linux checks and the 12-process concurrency workloads on the exact final source SHA.
- [ ] Inspect the final module and executable dependency records, dependency notices, and CGO setting for unexpected runtime requirements.
- [ ] For issue #14, install the downloaded release executable into an isolated environment and prove it runs without a separately installed Go or Cobra runtime.
- [ ] Preserve the adopted MIT license, positive attribution, human merge, and independent final-SHA verification.

### Historical limitations

This local candidate is not a public release artifact, so downloaded-binary installation and packaging remain pending issue #14.
The repository adopts the MIT license in `LICENSE`.
The source canary did not instrument transport request count, and it makes no claim about untested provider routes or future source behavior.
The comparator, tokenizer, and direct-file instruction trial are developer evidence rather than runtime dependencies or native skill discovery.
