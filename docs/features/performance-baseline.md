# Performance baseline

Status: implemented for the available local Cobra CLI and deterministic test fixture.

The baseline is a small developer measurement path rather than a benchmark service.

`mise run benchmark` builds the release-like binary, runs `go test -bench -benchmem` for in-process CLI allocations, and runs `scripts/benchmark.sh` for full-process measurements.

The full-process driver starts the compiled binary once per sample and records the exact arguments, exit code, stdout, stderr, output byte counts, structural token counts, output hashes, elapsed time, filesystem-cache label, subprocess count, and request count.

The report preserves environment and build metadata, binary size, raw samples, and per-workload count, minimum, maximum, p50, p95, mean, and dispersion.

The deterministic fixture seed and its SHA-256 are included in the metadata so repeated measurements can be tied to the same input.

The first sample is labeled `first-process` and later samples are labeled `warm-filesystem`.

No cache is purged and no provider, credential, network, telemetry, or live load loop is used.

## Scenarios

| ID | Description | Driver | Evidence |
| --- | --- | --- | --- |
| bench-stats | Seeded summary statistics retain count, extrema, p50, p95, mean, and dispersion. | automated `internal/benchmark/benchmark_test.go` | `go test ./internal/benchmark` |
| bench-tokenizer | The declared offline-word-v1 tokenizer produces deterministic equivalent-output counts without claiming model-token parity. | automated `internal/benchmark/benchmark_test.go` | `go test ./internal/benchmark` |
| bench-cli | The compiled Cobra entrypoint measures help, version, unavailable, and invalid-freshness output with expected exit codes and zero provider requests. | automated `scripts/benchmark.sh` and `cmd/remainder-benchmark/main.go` | `artifacts/benchmark-cli.jsonl` |
| bench-renderers | Compact, JSON, and scalar fixture output stays deterministic while the CLI benchmark records in-process allocations. | automated `internal/cli/cli_bench_test.go` | `artifacts/benchmark-in-process.txt` |
| bench-cache | Eligible-cache reads are unimplemented pending issue #6. | manual not-applicable: cache is not implemented in this release | issue #6 |
| bench-refresh | Controlled loopback refresh is unimplemented pending issue #5. | manual not-applicable: provider refresh is not implemented in this release | issue #5 |

## Measurement limits

The tokenizer is `offline-word-v1`, a whitespace-delimited structural tokenizer implemented with the Go standard library.

Its counts are comparable within this baseline only.

Another model's tokenizer is unmeasured, so this baseline makes no cross-model token-efficiency claim.

The `go test -bench -benchmem` artifact is the source for in-process allocations and bytes per operation.

Hosted CI timings are trends and are not a certification of the Apple Silicon full-process p95 objective.

The cache-read p95 objective remains unmeasured until issue #6 provides an eligible cache.
