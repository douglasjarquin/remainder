# Performance baseline

Status: implemented for the available local Cobra CLI and deterministic test fixture.

The baseline is a small developer measurement path rather than a benchmark service.

`mise run benchmark` builds the release-like binary, runs `go test -bench -benchmem` for in-process CLI allocations, and runs `scripts/benchmark.sh` for full-process measurements.

The full-process driver starts the compiled binary once per sample and records the exact arguments, exit code, stdout, stderr, output byte counts, optional model-token counts, output hashes, elapsed time, filesystem-cache label, subprocess count, and request count.

The report preserves environment and build metadata, binary size, raw samples, and per-workload count, minimum, maximum, p50, p95, mean, and dispersion.

The deterministic fixture corpus, its fixed rendering clock, and its SHA-256 are included in the metadata so repeated measurements can be tied to the exact input.

The fixture corpus emits healthy, exhausted, stale, partial-unknown, and shared-percent observations through compact, JSON, and scalar Cobra paths.

Compact and JSON records share the declared required-facts comparison scope.

The public cache policy adds four flags to every real Cobra command tree; the in-process compact, JSON, and scalar allocation ceilings include that command-construction cost.
Without race instrumentation, the measured compact, JSON, and scalar allocation counts were 136, 124, and 139.
With the canonical race-enabled test command, three runs observed compact at 139-140, JSON at 130-132, and scalar at 142 allocations; the proposed ceilings are 141, 133, and 143.

Scalar records are explicitly labeled as selected-value projections and are not claimed to be equivalent to the full observation.

The first sample is labeled `first-process` and later samples are labeled `warm-filesystem`.

The normal benchmark does not purge a cache or access credentials, network, telemetry, or a provider.

The optional comparator invokes quota-axi only inside its synthetic fetch harness and writes only to an owned temporary cache that it verifies and removes.

## Scenarios

| ID | Description | Driver | Evidence |
| --- | --- | --- | --- |
| bench-stats | Seeded summary statistics retain count, extrema, p50, p95, mean, and dispersion. | automated `internal/benchmark/benchmark_test.go` | `go test ./internal/benchmark` |
| bench-tokenizer | The optional pinned `tiktoken` bridge measures actual `o200k_base` tokens offline and records its identity and applicability. | manual optional measurement through `scripts/benchmark_tokens.py` and `scripts/benchmark.sh` with `REMAINDER_TOKENIZER_PYTHON` | `artifacts/benchmark-cli.jsonl` |
| bench-cli | The compiled Cobra entrypoint measures help, version, unavailable, and invalid-freshness output with expected exit codes and exact output regression checks. | automated `scripts/benchmark.sh` and `cmd/remainder-benchmark/main.go` | `artifacts/benchmark-cli.jsonl` |
| bench-renderers | Compact, JSON, and scalar fixture output stays observable across healthy, exhausted, stale, and partial-unknown states while the CLI benchmark records in-process allocations. | automated `internal/cli/cli_bench_test.go` and the benchmark driver | `artifacts/benchmark-cli.jsonl` and `artifacts/benchmark-in-process.txt` |
| bench-comparator | The optional pinned installed `quota-axi` compact command and its JSON output through the actual Pinchos `jq -r` consumer path run with a fixed synthetic response, sanitized homes, and a temporary cache. | manual optional probe through `scripts/benchmark.sh` with `REMAINDER_BENCH_COMPARATORS=1` | `artifacts/benchmark-cli.jsonl` |
| bench-budgets | Seeded compact, JSON, and scalar allocation budgets detect deterministic allocation regressions. | automated `internal/cli/cli_bench_test.go` | `go test -race -shuffle=on -count=1 ./...` |
| bench-cache | Atomic eligible-cache reads are implemented; the actual release-binary workload is completed by the root-owned issue #6 benchmark integration. | automated `internal/cache/` and `internal/cli/issue6_test.go`; release measurement pending root integration | issue #6 |
| bench-refresh | Controlled loopback refresh is unimplemented pending issue #5. | manual not-applicable: provider refresh is not implemented in this release | issue #5 |

## Measurement limits

The normal Go verification path does not import or require a tokenizer package.

For actual token measurements, provide an isolated Python environment with pinned `tiktoken`, provision the expected encoding data under `TIKTOKEN_CACHE_DIR`, and run `scripts/benchmark.sh` with `REMAINDER_TOKENIZER_PYTHON`.

The tokenizer bridge verifies that provisioned data matches the expected encoding hash and fails closed on a cache miss or mismatch without downloading or writing data.

The recorded `o200k_base` counts are applicable to the declared Codex-family comparison encoding, not a model-specific tokenizer certification.

If the optional tokenizer is omitted, token fields are explicitly unmeasured rather than replaced with word counts.

The optional comparator executes the pinned installed quota-axi package with `--no-credential-refresh` under synthetic `HOME`, `CODEX_HOME`, and `XDG_CACHE_HOME` directories.

A developer-only Node preload fixes the clock and intercepts global `fetch` before quota-axi loads, so no real provider, credential, cache, or installed package is accessed or modified.

The fixture response uses quota-axi's real `rate_limit.primary_window` and `secondary_window` input shape.

Both the compact command and JSON command are genuine quota-axi runs, and the JSON stdout is piped to the real Pinchos `jq -r` projection.

The metadata records the fresh temporary cache snapshot and measures Node preload startup separately as harness overhead.

It also records successful removal of the owned temporary sandbox after capture.

Comparator elapsed times include the harness and are observations only, not native endpoint performance or a speed-advantage claim.

The shared comparison is deliberately limited to provider, identical observation time and age, freshness, account/all-model scope, weekly window identity, percentage unit, and remaining value.

Remainder's profile, historical account binding, source, outcome, and value state remain extra facts, while quota-axi adds a constraining five-hour session window, reset times, plan, and pace.

The report preserves the scope and provenance uncertainty and does not claim full-payload equality or interchange token and percentage units.

The `go test -bench -benchmem` artifact is the source for in-process allocations and bytes per operation.

Hosted CI timings are trends and are not a certification of the Apple Silicon full-process p95 objective.

The driver does not apply the cache-read objective to startup or failure workloads.

The cache-read p95 objective remains unmeasured until the issue #6 release-binary benchmark integration records the eligible cache workload.

Full-process startup and failure p95 values are retained as hosted trend evidence and do not gate the benchmark.

An optional `--latency-baseline` JSON file enables seeded p95 regression detection for explicitly supplied workloads.

That detector is independent of the deferred eligible-cache 10 ms objective and never applies that objective to startup trends.
