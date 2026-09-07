# Feature map

| Feature | Status | Evidence or next issue |
| --- | --- | --- |
| `--help` and `--version` | Implemented | CLI tests and empty-home manual transcript in issue #2 evidence |
| Honest unavailable-provider result | Implemented | CLI test and default invocation return a nonzero exit without a fake report |
| Provider collection | Planned | Issue #5 starts after the output contract and credential route are proven |
| Compact, JSON, and scalar output | Implemented | Typed observations, deterministic renderers, exact selectors, and Cobra entrypoint tests in issue #3 |
| Cache and refresh ownership | Planned | Issues #6 and #7 define these boundaries |
| Supported macOS/Linux verification | Implemented | `.github/workflows/ci.yml` uses macOS 14 and Ubuntu 24.04 |
| Startup and allocation baseline | Implemented | `scripts/benchmark.sh` and `internal/cli/cli_bench_test.go` record raw samples |
