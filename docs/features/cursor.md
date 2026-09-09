# Cursor Linux file collection

Status: implemented with controlled file and HTTP fixtures; native canary unverified.
The current macOS environment has no authorized file-only Cursor route.
This increment does not certify native quota access or change the released v0.1.0 Codex artifact.

## Selection and source boundary

On Linux, use `remainder --provider cursor --profile default --format json`.
The source selects `CURSOR_CLI_CONFIG`, then `XDG_CONFIG_HOME/cursor/auth.json`, otherwise `$HOME/.config/cursor/auth.json`.
An explicitly empty override is invalid.
Only the selected bounded, owned regular file is read; symlinks, nonregular files, and metadata changes during reading are rejected.
The access token is never refreshed, and there is no login, external collector, Keychain, SQLite, or browser fallback.

The macOS CLI Keychain route and editor database routes are unsupported.
An explicit valid Cursor selection on macOS returns unavailable before credential access.
Invalid provider, profile, or account selection is rejected before source access.
`--all` includes Cursor after Codex, Claude, and Grok on Linux, and retains those three providers on macOS.

Collection calls `GetCurrentPeriodUsage`, `GetPlanInfo`, and `GetSandUsageStatus` on the fixed `https://api2.cursor.sh` DashboardService endpoint.
The three requests share a 15-second budget, each response is bounded to 1 MiB, and redirects are refused.
A supplemental failure may retain primary usage as partial evidence; caller cancellation returns no observation.
A 401 from any quota request invalidates the source and its cached evidence rather than returning partial quota.
The observation timestamp is captured when primary usage is obtained and is not advanced while supplemental requests run.

Account identity remains `unknown` with no identifier, including after cache reuse.
An explicit account selector cannot be verified and fails before HTTP.
The normal shared cache and refresh lock retain the original observation timestamp and source binding.

## Windows and units

Read exact window IDs from JSON before selecting a scalar.
`included_usage`, `auto_usage`, and `api_usage` preserve separate percentage allowances.
For example, `remainder value --provider cursor --profile default --window included_usage --field remaining` returns that allowance when defined.
`spend_limit` uses native `usd_cents` for supplied limit, used, and remaining amounts; it is never pooled with included percentages or treated as permission to spend.
`grok_bot` preserves the separate percentage window when supplied and not marked as pooled enterprise allowance.
Missing fields remain unknown and supplied zero remains zero.

Numeric values must use JSON decimal syntax, including quoted decimal and exponent forms.
Paid amounts retain their source precision within the shared evidence schema's finite numeric range.
Valid source reset timestamps are retained; a cycle too long for a Go duration has no duration rather than a saturated fabricated interval.
Unknown entitlements are not inferred from a plan name.

## Implementation and scenarios

`internal/cursor/` owns bounded credential access, private source decoding, normalization, and fixtures.
`internal/cli/cache_adapter.go` and `internal/cli/mixed.go` reuse the existing runtime and aggregate paths.

| ID | Description | Driver | Evidence |
| --- | --- | --- | --- |
| cursor-collection | Included, auto, API, spend, and Grok Bot values retain source units, precision, unknowns, zeros, and reset provenance. | automated `internal/cursor/adapter_test.go` and `internal/cursor/numeric_boundary_test.go` | `go test -race -shuffle=on -count=1 ./internal/cursor` |
| cursor-boundaries | File ownership and stability, explicit overrides, unsupported OS, selectors, malformed responses, redirects, 401/403/429, cancellation, and partial supplements are bounded. | automated `internal/cursor/auth_test.go`, `internal/cursor/adapter_test.go`, `internal/cursor/hardening_test.go`, and `internal/cursor/supplemental_auth_test.go` | `go test -race -shuffle=on -count=1 ./internal/cursor` |
| cursor-cli | Cobra selection, platform support, cache identity, output formats, and mixed failure behavior preserve earlier providers. | automated `internal/cli/` | `go test -race -shuffle=on -count=1 ./internal/cli` |
| cursor-native-canary | A usable authorized Linux CLI file must establish the actual quota route before native certification. | manual source-specific canary | Unverified: the current macOS environment has no authorized file-only token route. |

The [source matrix](../provider-sources.md) records the authorization boundary and pinned upstream implementation.
The private RPC schema is inferred from quota-axi commit `a19268827220e12e173067d11703e6ee36d5d88f`, not a published first-party protocol contract.
Controlled fixtures prove implementation behavior only.
