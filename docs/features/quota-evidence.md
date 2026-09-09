# Quota evidence

Status: implemented for deterministic fixtures, unavailable behavior, and controlled native Codex collection.

## Entry points

- `remainder --help` exposes the root report, cache policy flags, and the `value` command.
- `remainder --format compact` renders one deterministic compact observation.
- `remainder --format json` renders one versioned JSON observation.
- `remainder value --provider PROVIDER --profile PROFILE --window WINDOW --field FIELD` emits one exact scalar value.
- `internal/cli/run.go` and `internal/cli/cache_adapter.go` own Cobra parsing, cache/provider orchestration, output routing, and exit semantics.
- `internal/evidence/evidence.go`, `internal/evidence/parse.go`, `internal/evidence/render.go`, and `internal/evidence/select.go` own the typed observation, parsing, projections, and exact selection.
- `internal/codex/adapter.go`, `internal/codex/auth.go`, `internal/codex/source.go`, and `internal/codex/normalize.go` own the selected native source, bounded I/O, source schema, and normalization.
- `internal/cache/` owns versioned complete observations, source/auth metadata bindings, stable kernel locking, eligibility, and atomic replacement.

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
| cache-policy | Invalid modes, negative ages, and incompatible off/only options fail before source or cache work. | automated `internal/cli/issue6_test.go` | `go test -race -shuffle=on -count=1 ./internal/cli` |
| cache-hit | An eligible cached-only hit returns the complete observation with its original time and historical identity without OAuth parsing, lock waiting, or provider access. | automated `internal/cache/store_test.go`, `internal/cache/process_lock_test.go`, `internal/cli/issue6_test.go`, and `internal/codex/cache_binding_test.go` | `go test -race -shuffle=on -count=1 ./internal/cache ./internal/codex ./internal/cli` |
| cache-refresh | Misses, expiry, future clocks, forced generations, stale-on-transient-error, revocation, and account mismatch preserve age and identity policy. | automated `internal/cache/store_errors_test.go`, `internal/cache/store_concurrency_test.go`, and `internal/cli/issue6_test.go` | `go test -race -shuffle=on -count=1 ./internal/cache ./internal/cli` |
| cache-storage | Corrupt, partial, unsupported-schema, unavailable-storage, killed-writer, and concurrent older-write cases never expose partial data or overwrite newer/unknown records. | automated `internal/cache/store_errors_test.go`, `internal/cache/store_concurrency_test.go`, and `internal/cache/process_lock_test.go` | `go test -race -shuffle=on -count=1 ./internal/cache` |
| cache-process | A compiled helper process drives two actual Cobra executions against one temp auth/cache root and makes one controlled provider request. | automated `internal/cli/issue6_test.go` | `go test -race -shuffle=on -count=1 ./internal/cli` |

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

An eligible cache hit reads only the selected auth file metadata and the bounded cache record.
It retains the provider observation time and labels identity as historical because metadata does not prove the current login.

An uncached Codex/default request reads only the selected auth file and performs one bounded read-only usage operation.

## Gotchas and manual gaps

The observation records last-observed account identity separately from freshness and binding.

Cached evidence does not prove current login or an unobserved remote revocation.
An observed 401/403 or account mismatch blocks stale fallback until a successful refresh replaces the record or the source binding changes.

The cache root is `os.UserCacheDir()/remainder/v1/<binding-hash>/`.
Directories use mode `0700`, records and the never-unlinked stable lock use `0600`, records are bounded, and replacement uses a synced same-directory temporary file plus atomic rename.
Credential values, raw auth payloads, and absolute auth paths are not serialized.

A consumer presentation cache adds to the age of Remainder's original observation.
Consumers should use a short Remainder reuse window for nearby field reads rather than stacking another five-minute TTL and calling the total age five minutes.

Codex provider collection is implemented from the selected native file source.
Two authorized native observations passed on 2026-09-09 with the same verified account binding and unchanged credential file metadata.
This verifies the selected macOS route at the recorded source revision; see the [source evidence](../provider-sources.md).

Other provider routes and packaged-release acceptance remain separate work; see the [provider source matrix](../provider-sources.md).
