# Quota evidence

Status: implemented for deterministic fixtures, unavailable behavior, controlled native Codex collection, and per-window pace derivation.

## Entry points

- `remainder --help` exposes the root report flags and the `value` command.
- `remainder --format compact` renders one deterministic compact observation.
- `remainder --format json` renders one versioned JSON observation.
- `remainder value --provider PROVIDER --profile PROFILE --window WINDOW --field FIELD` emits one exact scalar remaining, reset, duration, or pace value.
- `internal/cli/run.go` owns Cobra parsing, adapter calls, output routing, and exit semantics.
- `internal/evidence/evidence.go`, `internal/evidence/parse.go`, `internal/evidence/render.go`, and `internal/evidence/select.go` own the typed observation, parsing, projections, and exact selection.
- `internal/evidence/pace.go` derives per-window pace from one observation and a caller-supplied evaluation clock.
- `internal/codex/adapter.go`, `internal/codex/auth.go`, `internal/codex/source.go`, and `internal/codex/normalize.go` own the selected native source, bounded I/O, source schema, and normalization.

## Scenarios

| ID | Description | Driver | Evidence |
| --- | --- | --- | --- |
| quota-help | Help prints the command surface to stdout without observing a provider. | automated `internal/cli/cli_test.go` and `internal/cli/issue3_test.go` | `go test -race -shuffle=on -count=1 ./...` |
| quota-invalid | Invalid freshness and incomplete value selection fail with exit 2, stderr diagnostics, and no usage text. | automated `internal/cli/issue3_test.go` and `bin/remainder --freshness ignored` | `artifacts/issue-3/cli/final-invalid.stderr` |
| quota-compact | Compact output is deterministic, escaped, age-aware, and limited to the requested weekly model scope. | automated `internal/evidence/evidence_test.go` and `internal/cli/issue3_test.go` | `artifacts/issue-3/green-compact-cli.txt` |
| quota-json | JSON preserves schema, source, account, freshness, outcome, failures, windows, units, values, resets, and durations. | automated `internal/evidence/evidence_test.go` and `internal/cli/issue3_test.go` | `go test -race -shuffle=on -count=1 ./...` |
| quota-scalar | Exact scalar selection emits defined and zero values plus unlimited state, while unknown, stale, wrong-account, and ambiguous data fail. | automated `internal/evidence/evidence_test.go` and `internal/cli/issue3_test.go` | `artifacts/issue-3/green-focused-tests.txt` |
| quota-malformed | Malformed timestamps and duplicate IDs are rejected before a value can be emitted. | automated `internal/evidence/evidence_test.go` | `artifacts/issue-3/green-focused-tests.txt` |
| quota-partial | Partial evidence is rendered as data and returns a distinct exit 3 with one stderr diagnostic. | automated `internal/cli/issue3_test.go` | `artifacts/issue-3/green-focused-tests.txt` |
| quota-unavailable | The shipped binary reports honest unavailable behavior with exit 1 and empty stdout. | automated `internal/cli/cli_test.go` and `bin/remainder` | `artifacts/issue-3/cli/final-unavailable.stderr` |
| quota-fresh-trees | Repeated command execution uses fresh Cobra trees and does not leak adapter or flag state. | automated `internal/cli/issue3_test.go` | `artifacts/issue-3/green-focused-tests.txt` |
| codex-native-healthy | Selected synthetic auth and controlled TLS usage responses render compact, JSON, and exact scalar results through Cobra. | automated `internal/codex/codex_test.go` and `internal/cli/issue5_test.go` | `go test -race -shuffle=on -count=1 ./internal/codex ./internal/cli` |
| codex-native-bounds | Missing, malformed, expired, wrong-profile, wrong-account, delayed, oversized, malformed, redirected, rejected, rate-limited, canceled, and schema-drift inputs fail safely without secret disclosure. | automated `internal/codex/codex_test.go` | `go test -race -shuffle=on -count=1 ./internal/codex` |
| codex-native-process | A compiled helper process drives the actual Cobra entrypoint against a controlled HTTP source and emits the exact scalar. | automated `internal/cli/issue5_test.go` | `go test -race -shuffle=on -count=1 ./internal/cli` |
| quota-pace | Percentage windows classify spending as `ahead`, `on_pace`, or `behind` from the same remaining/reset/duration observation, including inclusive threshold ties and zero remaining. | automated `internal/evidence/pace_test.go` | `go test -race -shuffle=on -count=1 ./internal/evidence` |
| quota-pace-unknown | Stale, missing, expired-reset, future-cycle, unlimited, unknown, malformed, and ambiguous inputs preserve an explicit unknown reason without a manufactured scalar. | automated `internal/evidence/pace_test.go` | `go test -race -shuffle=on -count=1 ./internal/evidence` |
| quota-pace-process | A compiled helper process drives the actual Cobra entrypoint against a controlled Codex HTTP fixture and emits the exact weekly pace scalar. | automated `internal/cli/issue11_test.go` | `go test -race -shuffle=on -count=1 ./internal/cli` |

## Driving it

Run `mise run build` and then drive the root and `value` commands as one-shot processes.

Injectable adapter fixtures are internal test seams only and are not exposed as a public fake-provider mode.

## Expected states and side effects

Exit 0 means usable evidence, including zero and unlimited values.

Exit 1 means the observation is unavailable.

Exit 2 means invocation or selection is invalid, including stale-disallowed, wrong-account, ambiguous, undefined, malformed, and duplicate evidence.

Exit 3 means partial evidence was rendered.

Exit 130 means interruption.

Help, version, and invalid input perform no adapter work.

Help, version, and validation perform no credential, provider, cache, network, telemetry, or account-binding operation.

An explicit Codex/default request reads only the selected auth file and performs one bounded read-only usage operation.

Pace is calculated independently for each applicable percentage window.
The calculation assumes uniform allowance through the cycle and uses the original observation time, remaining percentage, reset, and duration.
It records the current calculation time so a cached observation can be re-evaluated after freshness classification.
Stale evidence and a reset that has passed at evaluation time are unknown even when an earlier calculation was known.
The signed reserve is percentage remaining minus cycle time remaining percentage: below `-1` is `ahead` spending faster, above `1` is `behind` spending slower, and both threshold ties are `on_pace`.
This matches [quota-axi's formula and status meanings at pinned revision `d3190237588cdf51046b27a346ff2e834855bf37`](https://github.com/kunchenguid/quota-axi/blob/d3190237588cdf51046b27a346ff2e834855bf37/src/pace.ts#L38-L69); Remainder intentionally omits burn forecasts and aggregate selection scores because issue #11 requires scalar status without cross-pool prediction.
Account, model, short-window, unknown, zero, and unlimited constraints remain separate.
No minimum is selected across incomparable units or pools, and credits are not added to included allowance.

## Gotchas and manual gaps

The observation records last-observed account identity separately from freshness and binding.

This release does not prove current login, revocation, or credential binding.

Codex provider collection is implemented from the selected native file source.
Two authorized native observations passed on 2026-09-09 with the same verified account binding and unchanged credential file metadata.
This verifies the selected macOS route at the recorded source revision; see the [source evidence](../provider-sources.md).

Other provider routes and packaged-release acceptance remain separate work; see the [provider source matrix](../provider-sources.md).
