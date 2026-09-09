# Grok file collection

Status: implemented with controlled file and HTTP fixtures; native canary unverified.
The authorized default credential file contained an expired consumer session during the 2026-09-09 preflight.
This increment does not certify a live Grok route or change the released v0.1.0 Codex artifact.

## Selection and identity

Use `remainder --provider grok --profile default --format json` to inspect the selected observation.
The source selects `GROK_AUTH_JSON`, then `GROK_AUTH_PATH`, then `GROK_HOME/auth.json`, otherwise `$HOME/.grok/auth.json`.
An explicitly empty override is invalid, and inline `GROK_AUTH` is unsupported.
The source opens only the selected owned, bounded regular file and refuses symlinks.
It never invokes another CLI, login, refresh, Keychain, SQLite, or a browser credential store.

The only quota operation is the consumer `GetGrokCreditsConfig` RPC on `grok.com`.
An xAI API key or successful model-list request is not consumer quota evidence and is not a fallback.
The operation has a 15-second timeout, a 64 KiB response bound, and no redirect following.

The returned account binding is `unknown` with no account identifier.
Local email and team hints do not verify the quota account.
An explicit `--account` therefore fails before quota HTTP.
Cache hits retain unknown identity and the original observation timestamp.

## Windows and units

Read exact window IDs from JSON before selecting a value.
Window `credits` describes the shared percentage allowance, with `used` and `remaining` fields.
For example, `remainder value --provider grok --profile default --window credits --field remaining` selects that percentage when defined.

Product windows use `product:<kind>` for both ID and scope.
Known kinds include `api`, `grok_build`, `grok_plugins`, `chat`, `imagine`, and `voice`; unfamiliar numeric kinds remain distinct.
Window `prepaid` contains a separate `remaining` credit balance.
No shared, product, or prepaid pools are added together, and credits are not converted to tokens.
Absent values remain unknown while measured zero remains zero.
Source-supplied weekly or monthly start/reset pairs provide cycle provenance; malformed or unsupported periods fail conservatively.

## Mixed reports

`remainder --all --format json` reads the default Codex, Claude, and Grok contexts concurrently and emits them in that order.
It does not discover additional profiles.
Do not combine `--all` with `--provider`, `--profile`, or `--account`.
Window and scope filters apply separately to each provider.

The JSON envelope contains `outcome`, `observations`, and provider-scoped `failures`.
Each observation retains the ordinary single-provider schema, identity, units, and timestamps.
Compact mixed output is one line containing quoted provider reports.
One failed provider preserves the other observations and returns exit 3; no usable observations returns exit 1 with empty stdout.
A partially defined provider observation also makes the mixed result partial.

Cache policy is applied independently to each provider.
A fresh cache hit that exceeds `--max-age` while another provider is still running is excluded at report assembly rather than re-dated.
This does not trigger a second provider request.
Explicit stale-on-error evidence remains subject to the selected freshness policy.

## Implementation

`internal/grok/` owns bounded native credential reads, the private wire decoder, normalization, and provider-level fixtures.
`internal/cli/report.go` preserves single-provider output and dispatches mixed reports to `internal/cli/mixed.go`.

## Scenarios

| ID | Description | Driver | Evidence |
| --- | --- | --- | --- |
| grok-collection | Shared and product percentages, cycles, zero, unknown, and prepaid credits retain their native meanings. | automated `internal/grok/adapter_test.go`, `internal/grok/source_test.go`, and `internal/grok/duration_boundary_test.go` | `go test -race -shuffle=on -count=1 ./internal/grok` |
| grok-boundaries | Credential precedence, expired and ambiguous sessions, API keys, nonregular files, redirects, oversized responses, cancellation, and malformed protobuf are bounded. | automated `internal/grok/adapter_test.go`, `internal/grok/wire_test.go`, and `internal/grok/source_test.go` | `go test -race -shuffle=on -count=1 ./internal/grok` |
| grok-cache-binding | Selected source metadata isolates cache records and classifies authorization and retry failures. | automated `internal/grok/cache_failure_test.go` | `go test -race -shuffle=on -count=1 ./internal/grok` |
| grok-cli | Compact, JSON, scalar, missing-source, explicit account, and eligible cache behavior run through Cobra and native Grok fixtures. | automated `internal/cli/grok_test.go` | `go test -race -shuffle=on -count=1 ./internal/cli` |
| grok-mixed | Provider ordering, scoped failures, unchanged observation wire format, cache age, selection conflicts, and cancellation preserve usable evidence. | automated `internal/cli/mixed_test.go`, `internal/cli/mixed_runtime_test.go`, and `internal/cli/mixed.go` | `go test -race -shuffle=on -count=1 ./internal/cli` |
| grok-native-canary | A usable authorized default consumer file must establish the actual quota route before native certification. | manual source-specific canary | Unverified: selected session expired; refresh and alternate routes are excluded. |

The [source matrix](../provider-sources.md) pins the upstream research and authorization boundary.
The private RPC schema is inferred from that implementation, not a published first-party protocol contract.
Controlled fixtures prove implementation behavior only.
