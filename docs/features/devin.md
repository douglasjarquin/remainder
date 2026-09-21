# Devin file collection

Status: implemented with controlled file and HTTP fixtures; the file route passed a native canary on this host.
On 2026-09-21 the selected `default` route was exercised against a real installed and authenticated Devin CLI (`devin 3000.10.31`, `~/.local/bin/devin`) on this macOS host.
This source increment does not change any released artifact.

## Selection and identity

Use `remainder --provider devin --profile default --format json` to inspect the selected observation.
The `default` profile means the single Devin CLI credential file selected by the process environment.
The resolution order is `DEVIN_CREDENTIALS`, then `$XDG_DATA_HOME/devin/credentials.toml` when the override is unset, then `$HOME/.local/share/devin/credentials.toml`.
An explicitly empty `DEVIN_CREDENTIALS` is invalid.
The file is the Devin CLI's own TOML credentials file; Remainder reads `windsurf_api_key` and the `api_server_url` host it names, defaulting to `https://server.codeium.com` when the key is absent.
There is no login, CLI invocation, token refresh, or Keychain fallback.

A cache miss sends one bounded Connect JSON `POST` to `<api server>/exa.seat_management_pb.SeatManagementService/GetUserStatus`, the same read-only status call the installed Devin CLI uses for `devin auth status`.
The request carries `metadata.apiKey` plus fixed client-identifying fields, `Content-Type: application/json`, and `Connect-Protocol-Version: 1`.
The response `userStatus.userId` becomes the observation account identity with a verified binding.
An explicit `--account` mismatch fails the observation; a missing `userId` keeps identity unknown and cannot satisfy an account selector.
Eligible cache hits retain the original observation timestamp and label previously verified identity as historical.

## Windows and units

Read window IDs from JSON before selecting values.
Account quota windows use `daily` and `weekly`, each carrying `used`, `remaining`, `duration`, and `reset` limits derived from `dailyQuotaRemainingPercent`/`weeklyQuotaRemainingPercent` and the `*QuotaResetAtUnix` timestamps.
Missing percentages remain unknown and mark the observation partial; a measured zero remains zero.

For example, `remainder value --provider devin --profile default --window weekly --field remaining` selects the account weekly percentage when defined.
The usual freshness, cache, scope, account, and pace rules apply to the same normalized observation.

ACU usage, when the plan reports `acuConsumed` or `acuLimit`, uses window ID `acu` with unit `acu`, `used` and `remaining` limits, and the `planEnd`/`planStart` cycle as reset and duration.
Prompt, flow, and flex credit balances, when reported, use window IDs `prompt_credits`, `flow_credits`, and `flex_credits` with unit `credits`.
The source's negative sentinel (`-1`) is interpreted as unlimited for `remaining` and `limit` fields, and makes an absent `used` value `not_applicable` rather than unknown.
A negative `used` value is unknown, and a bounded allowance with an unreported used value marks the observation partial.
No credit pool is combined with the percentage quota windows.

## Scenarios

| ID | Description | Driver | Evidence |
| --- | --- | --- | --- |
| devin-collection | Verified identity, daily and weekly quota windows, ACU and credit windows, unlimited sentinels, zero, unknown, and partial normalization through bounded Connect JSON HTTP. | automated `internal/devin/adapter_test.go` | `go test -race -shuffle=on -count=1 ./internal/devin` |
| devin-boundaries | Missing, malformed, nonregular, symlinked, oversized, section-scoped, duplicate-key, and foreign-URL credential files fail closed; canceled, oversized, malformed, unauthorized, rate-limited, and transient responses are bounded and classified. | automated `internal/devin/adapter_test.go` | `go test -race -shuffle=on -count=1 ./internal/devin` |
| devin-cli | Compact, JSON, scalar remaining and reset output, provider selection, account mismatch, absent credentials, 401 revocation, and 429 backoff preserve native CLI behavior. | automated `internal/cli/devin_test.go` | `go test -race -shuffle=on -count=1 ./internal/cli` |
| devin-cache | Eligible hits avoid credential parsing and HTTP; the credential file fingerprint binds the cache entry. | automated `internal/devin/adapter_test.go` and `internal/cli/devin_test.go` | `go test -race -shuffle=on -count=1 ./internal/devin ./internal/cli` |
| devin-native-canary | An authorized usable credential file must establish the actual source route before native certification. | manual source-specific canary | Verified 2026-09-21 on this macOS host against `devin 3000.10.31`: a cold `--provider devin --profile default --format json` observation returned exit 0, fresh complete evidence, `native_file_http`/`devin_credentials_toml`, verified `userStatus.userId` binding, daily and weekly 100-percent windows with resets and pace, and unlimited `prompt_credits`; a `value` scalar returned `100`; a same-run `--cache=only` hit reused the original `observed_at` and relabeled identity `historical`; `--all` collected devin alongside other providers with independent failures. |

The [source matrix](../provider-sources.md) records the authorization boundary.
The Connect JSON contract is inferred from the installed Devin CLI binary's embedded `SeatManagementService` descriptors and one authorized live read; no published first-party schema is assumed.
The documented enterprise consumption endpoints (`api.devin.ai` `/v3/enterprise/*`) require service-user permissions this Devin Pro account does not grant and are not used.
Controlled fixtures prove implementation behavior; the 2026-09-21 canary above is the separate live-route evidence.
