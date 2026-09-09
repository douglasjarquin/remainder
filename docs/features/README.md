# Feature map

Inventory: incomplete

The issue #3 contract is tracked in [quota evidence](quota-evidence.md).

The issue #4 baseline is tracked in [performance baseline](performance-baseline.md).

| Feature | Status | Evidence or next issue |
| --- | --- | --- |
| `--help` and `--version` | Implemented | CLI tests and empty-home manual transcript in issue #2 evidence |
| Honest unavailable-provider result | Implemented | CLI test and default invocation return a nonzero exit without a fake report |
| Provider source feasibility | Selected native Codex route observed twice | [Provider source matrix](../provider-sources.md); two read-only native observations with matching verified binding; other provider labs remain unrun |
| Provider collection | Codex implemented; selected native route verified | Bounded selected auth read, current native usage endpoint, normalization, and controlled HTTP/TLS tests in issue #5 |
| Compact, JSON, and scalar output | Implemented | Typed observations, deterministic renderers, exact selectors, and Cobra entrypoint tests in issue #3 |
| Cache and refresh ownership | Implemented | Issue #6 atomic observations and issue #7 cross-process success, forced-generation, and bounded failure coalescing use the same stable response lock |
| Supported macOS/Linux verification | Implemented | `.github/workflows/ci.yml` uses macOS 14 and Ubuntu 24.04 |
| Startup, output, token, allocation, cache, and contention baseline | Implemented for startup, native refresh, eligible cache hits, and coalesced refreshes | `scripts/benchmark.sh`, `scripts/benchmark_tokens.py`, `scripts/quota_axi_fixture.mjs`, `cmd/remainder-benchmark/`, and `internal/cli/cli_bench_test.go` record raw samples, actual optional tokens, controlled shared facts, summaries, output, allocations, cache-hit release processes, controlled native-refresh helper processes, and release-process contention with explicit request and process counts |
