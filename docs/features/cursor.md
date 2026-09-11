# Cursor CLI collection

Status: Linux file and macOS Keychain collection have controlled source and HTTP fixtures.
Native certification is source-specific.
A maintainer-run macOS CLI Keychain canary succeeded headlessly on 2026-09-09 at source commit `c8267eb582f74b50fcd0e201e9bd990a5b7e049a`, returning five separate fresh windows with caching disabled.
Linux native access and packaged provider release certification remain unverified.
The released v0.1.0 Codex artifact is unchanged.

## Selection and source boundary

On Linux, use `remainder --provider cursor --profile default --format json`.
The source selects `CURSOR_CLI_CONFIG`, then `XDG_CONFIG_HOME/cursor/auth.json`, otherwise `$HOME/.config/cursor/auth.json`.
An explicitly empty override is invalid.
Only the selected bounded, owned regular file is read; symlinks, nonregular files, and metadata changes during reading are rejected.
The access token is never refreshed, and there is no login, external collector, SQLite, or browser fallback.

On macOS, use `remainder --provider cursor --profile default --allow-keychain-prompt --format json` for a new observation.
The CLI identity file is selected by `CURSOR_CLI_CONFIG`, otherwise `$HOME/.cursor/cli-config.json`.
The source reads only the `cursor-access-token` item for account `cursor-user` through the system `/usr/bin/security` helper.
The flag authorizes a Keychain read for this invocation and may cause macOS to ask for approval.
Without it, a cache miss returns unavailable without invoking the helper.
There is no persistent consent marker, inherited quota-axi grant, ACL change, or guarantee that an opted-in read is noninteractive.
Help, cached-only reads, and eligible cache hits never call the helper, even when the flag is supplied.
Editor database routes remain unsupported.
Invalid provider, profile, or account selection is rejected before source access.
`--all` includes Cursor after Codex, Claude, and Grok on Linux and macOS.
On macOS, the Keychain opt-in applies only to Cursor collection; other provider credential policies are unchanged.

Collection calls `GetCurrentPeriodUsage`, `GetPlanInfo`, and `GetSandUsageStatus` on the fixed `https://api2.cursor.sh` DashboardService endpoint.
The three requests share a 15-second budget, each response is bounded to 1 MiB, and redirects are refused.
A supplemental failure may retain primary usage as partial evidence; caller cancellation returns no observation.
A 401 from any quota request invalidates the source and its cached evidence rather than returning partial quota.
The observation timestamp is captured when primary usage is obtained and is not advanced while supplemental requests run.

Account identity remains `unknown` with no identifier, including after cache reuse.
An explicit account selector cannot be verified and fails before HTTP.
The normal shared cache and refresh lock retain the original observation timestamp and source binding.
The macOS namespace is bound to the selected nonsecret CLI configuration, with a distinct Keychain source identity.
A Keychain-only change without a CLI configuration change cannot be detected on a cache hit; cached evidence never asserts current login or authorization.

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
| cursor-keychain | Explicit macOS permission, bounded helper execution, secret-safe failures, CLI configuration binding, and no helper work on cached reads. | automated `internal/cursor/keychain_test.go`, `internal/cursor/keychain_process_test.go`, and `internal/cli/cursor_darwin_test.go` | `go test -race -shuffle=on -count=1 ./internal/cursor ./internal/cli` |
| cursor-cli | Cobra selection, platform support, cache identity, output formats, and mixed failure behavior preserve earlier providers. | automated `internal/cli/` | `go test -race -shuffle=on -count=1 ./internal/cli` |
| cursor-native-canary | An authorized platform-specific CLI source must establish the actual quota route before native certification. | manual source-specific canary | Required before native release certification; retain exact binary and source-specific observation evidence. |

The [source matrix](../provider-sources.md) records the authorization boundary and pinned upstream implementation.
The private RPC schema is inferred from quota-axi commit `a19268827220e12e173067d11703e6ee36d5d88f`, not a published first-party protocol contract.
Controlled fixtures prove implementation behavior only.
