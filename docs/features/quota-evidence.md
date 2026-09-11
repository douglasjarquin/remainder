# Quota evidence

Status: implemented for deterministic local fixtures and unavailable runtime behavior.

## Entry points

- `remainder --help` exposes the root report flags and the `value` command.
- `remainder --format compact` renders one deterministic compact observation.
- `remainder --format json` renders one versioned JSON observation.
- `remainder --format toon` renders one TOON document of the same facts JSON preserves.
- `remainder value --provider PROVIDER --profile PROFILE --window WINDOW --field FIELD` emits one exact scalar value.
- `internal/cli/run.go` owns Cobra parsing, adapter calls, output routing, and exit semantics.
- `internal/evidence/evidence.go`, `internal/evidence/parse.go`, `internal/evidence/render.go`, and `internal/evidence/select.go` own the typed observation, parsing, projections, and exact selection.

## Scenarios

| ID | Description | Driver | Evidence |
| --- | --- | --- | --- |
| quota-help | Help prints the command surface to stdout without observing a provider. | automated `internal/cli/cli_test.go` and `internal/cli/issue3_test.go` | `go test -race -shuffle=on -count=1 ./...` |
| quota-invalid | Invalid freshness and incomplete value selection fail with exit 2, stderr diagnostics, and no usage text. | automated `internal/cli/issue3_test.go` and `bin/remainder --freshness ignored` | `artifacts/issue-3/cli/final-invalid.stderr` |
| quota-compact | Compact output is deterministic, escaped, age-aware, and limited to the requested weekly model scope. | automated `internal/evidence/evidence_test.go` and `internal/cli/issue3_test.go` | `artifacts/issue-3/green-compact-cli.txt` |
| quota-json | JSON preserves schema, source, account, freshness, outcome, failures, windows, units, values, resets, and durations. | automated `internal/evidence/evidence_test.go` and `internal/cli/issue3_test.go` | `go test -race -shuffle=on -count=1 ./...` |
| quota-toon | TOON preserves the same facts as JSON for healthy, zero, unknown, stale-disallowed, partial `--all`, and invalid `--format` cases. | automated `internal/evidence/evidence_test.go` and `internal/cli/issue46_test.go` | `go test -race -shuffle=on -count=1 ./...` |
| quota-scalar | Exact scalar selection emits defined and zero values plus unlimited state, while unknown, stale, wrong-account, and ambiguous data fail. | automated `internal/evidence/evidence_test.go` and `internal/cli/issue3_test.go` | `artifacts/issue-3/green-focused-tests.txt` |
| quota-malformed | Malformed timestamps and duplicate IDs are rejected before a value can be emitted. | automated `internal/evidence/evidence_test.go` | `artifacts/issue-3/green-focused-tests.txt` |
| quota-partial | Partial evidence is rendered as data and returns a distinct exit 3 with one stderr diagnostic. | automated `internal/cli/issue3_test.go` | `artifacts/issue-3/green-focused-tests.txt` |
| quota-unavailable | The shipped binary reports honest unavailable behavior with exit 1 and empty stdout. | automated `internal/cli/cli_test.go` and `bin/remainder` | `artifacts/issue-3/cli/final-unavailable.stderr` |
| quota-fresh-trees | Repeated command execution uses fresh Cobra trees and does not leak adapter or flag state. | automated `internal/cli/issue3_test.go` | `artifacts/issue-3/green-focused-tests.txt` |

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

The runtime performs no credential, provider, cache, network, telemetry, or account-binding operation.

## Gotchas and manual gaps

The observation records last-observed account identity separately from freshness and binding.

This release does not prove current login, revocation, or credential binding.

Provider collection remains planned for issue #5 and credential feasibility remains governed by issue #15.

The full product inventory and any live provider route remain manual work for later issues; see the [provider source matrix](../provider-sources.md).
