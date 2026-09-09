# Codex release readiness

Status: the source candidate is ready for independent verification, while downloaded executable proof remains the issue #14 release gate.

## Scope and evidence

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
The in-process allocation gates retained the fixed compact, JSON, remaining-scalar, and pace-scalar ceilings of 130, 120, 125, and 125 allocations.
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
- [ ] Keep the allocation ceilings at compact 130, JSON 120, remaining scalar 125, and pace scalar 125 unless a recorded investigation approves a change.
- [ ] Measure raw full-process samples through the CGO-free binary and require eligible Apple Silicon cache-hit p95 to remain at or below 10 ms.
- [ ] Record process, request, allocation, memory, output, and cleanup facts without combining local, harness, network, or auth latency.
- [ ] Run the pinned comparator and declared tokenizer only from pre-provisioned isolated environments, with external lookup disabled and uncertainty preserved.
- [ ] Keep one explicitly approved live Codex canary separate from fixtures and record the exact source SHA, selection, auth behavior, metadata stability, and cleanup.
- [ ] Re-run supported macOS and Linux checks and the 12-process concurrency workloads on the exact final source SHA.
- [ ] Inspect the final module and executable dependency records, dependency notices, and CGO setting for unexpected runtime requirements.
- [ ] For issue #14, install the downloaded release executable into an isolated environment and prove it runs without a separately installed Go or Cobra runtime.
- [ ] Preserve the pending owner license decision, positive attribution, human merge, and independent final-SHA verification.

## Limitations

No public release artifact exists in this readiness slice, so downloaded-binary installation and packaging remain pending issue #14.
The MIT text in `LICENSE_PROPOSAL.md` is still a proposal and has not been adopted.
The source canary did not instrument transport request count, and it makes no claim about untested provider routes or future source behavior.
The comparator, tokenizer, and direct-file instruction trial are developer evidence rather than runtime dependencies or native skill discovery.
