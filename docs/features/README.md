# Feature map

Inventory: incomplete

The issue #3 contract is tracked in [quota evidence](quota-evidence.md).

The issue #4 baseline is tracked in [performance baseline](performance-baseline.md).

| Feature | Status | Evidence or next issue |
| --- | --- | --- |
| `--help` and `--version` | Implemented | CLI tests and empty-home manual transcript in issue #2 evidence |
| Honest unavailable-provider result | Implemented | CLI test and default invocation return a nonzero exit without a fake report |
| Provider source feasibility | Selected native Codex and macOS Cursor routes observed | [Provider source matrix](../provider-sources.md); Codex verified binding and Cursor Keychain canary; other native provider gates remain open |
| Provider collection | Codex native route and macOS Cursor CLI Keychain canary verified; Claude, Grok, and Linux Cursor implementations verified with controlled fixtures only | Bounded selected auth read, current native usage endpoint, normalization, and controlled HTTP/TLS tests in issue #5 |
| Claude file collection | Implemented; native canary unverified | [Claude source, selectors, and fixture coverage](claude.md) |
| Grok file collection and mixed reports | Implemented; native canary unverified | [Grok source, scopes, and mixed reports](grok.md) |
| Cursor CLI collection | Implemented; native certification is source-specific | [Cursor source, platform boundary, and fixture coverage](cursor.md) |
| Compact, JSON, and scalar output | Implemented | Typed observations, deterministic renderers, exact selectors, and Cobra entrypoint tests in issue #3 |
| Codex remaining and pace semantics | Implemented | Per-window percentage pace with source-preserving inputs, exact scalar status, and all account/model/short constraints retained in issue #11 |
| Cache and refresh ownership | Implemented | Issue #6 atomic observations and issue #7 cross-process success, forced-generation, and bounded failure coalescing use the same stable response lock |
| Standalone Codex skill | Implemented in source | `skills/remainder/SKILL.md` delegates command discovery to Cobra help; `internal/cli/issue12_test.go` runs its ordinary-shell and direct Pinchos examples through a compiled deterministic fixture |
| Standalone release packaging | v0.1.0 published; v0.2.0 candidate includes macOS Cursor and requires its release record | `scripts/package-release.sh` builds the native archive, `scripts/verify-release-asset.sh` verifies it, and `scripts/package-release.test.sh` tests the archive, checksum, strict file allowlist including the MIT license and linked provider documents, clean runtime path, Cobra help/error behavior, source metadata, and pre-publication candidate manifest |
| Codex source release readiness | v0.1.0 release verified; checklist retained for later increments | [Release readiness record and reusable checklist](../release-readiness.md) |
| Supported macOS/Linux verification | Implemented | `.github/workflows/ci.yml` uses macOS 14 and Ubuntu 24.04 |
| Startup, output, token, allocation, cache, and contention baseline | Implemented for startup, native refresh, eligible cache hits, and coalesced refreshes | `scripts/benchmark.sh`, `scripts/benchmark_tokens.py`, `scripts/quota_axi_fixture.mjs`, `cmd/remainder-benchmark/`, and `internal/cli/cli_bench_test.go` record raw samples, actual optional tokens, controlled shared facts, summaries, output, allocations, cache-hit release processes, controlled native-refresh helper processes, and release-process contention with explicit request and process counts |
