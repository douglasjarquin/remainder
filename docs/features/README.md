# Feature map

Inventory: incomplete

The issue #3 contract is tracked in [quota evidence](quota-evidence.md).

The issue #4 baseline is tracked in [performance baseline](performance-baseline.md).

| Feature | Status | Evidence or next issue |
| --- | --- | --- |
| `--help` and `--version` | Implemented | CLI tests and empty-home manual transcript in issue #2 evidence |
| Honest unavailable-provider result | Implemented | CLI test and default invocation return a nonzero exit without a fake report |
| Provider source feasibility | Codex file shape proved; native endpoint canary unrun | [Provider source matrix](../provider-sources.md); the source-built route still requires an authorized read-only canary before release support is claimed |
| Provider collection | Codex implemented; live support unverified | Bounded selected auth read, two native usage endpoints, normalization, and controlled HTTP/TLS tests in issue #5 |
| Compact, JSON, and scalar output | Implemented | Typed observations, deterministic renderers, exact selectors, and Cobra entrypoint tests in issue #3 |
| Cache and refresh ownership | Planned | Issues #6 and #7 define these boundaries |
| Supported macOS/Linux verification | Implemented | `.github/workflows/ci.yml` uses macOS 14 and Ubuntu 24.04 |
| Startup, output, token, and allocation baseline | Partial | `scripts/benchmark.sh`, `scripts/benchmark_tokens.py`, `cmd/remainder-benchmark/main.go`, and `internal/cli/cli_bench_test.go` record raw samples, controlled fixture comparisons, optional actual tokens, summaries, output, and allocations; compact comparator acceptance awaits fixture-capable quota-axi input or the later provider/cache mapping |
