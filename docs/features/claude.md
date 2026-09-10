# Claude file collection

Status: implemented with controlled file and HTTP fixtures; native canary verified.
The authorized default credential file was absent during the 2026-09-09 preflight; on 2026-09-10 the same selected `default` route was authorized and exercised against a real installed and authenticated Claude Code CLI (`~/.local/bin/claude` 2.1.267) on `host-development.douglasjarquin.i-09b74c2aaae0cbbb8` (Linux).
This source increment does not change the released v0.1.0 Codex artifact.

## Selection and identity

Use `remainder --provider claude --profile default --format json` to inspect the selected observation.
The `default` profile means the single `CLAUDE_CONFIG_DIR` context selected by the process environment, or `$HOME/.claude` when the override is unset.
An explicitly empty override does not select the current directory or a fallback.
The source reads only the selected credential file and never invokes Keychain, another CLI, login, or refresh.

A cache miss first calls the Claude profile endpoint and requires `account.uuid`.
The observation account ID is `account.uuid@organization.uuid` when an organization UUID is supplied, otherwise `account.uuid`.
An explicit `--account` mismatch fails before the usage request.
Eligible cache hits retain the original observation timestamp and label previously verified identity as historical.
Unknown identity remains unknown in the shared cache.

## Windows and units

Read window IDs from JSON before selecting model-specific values.
Account session and weekly windows use `five_hour` and `weekly`.
Model windows use `model_<id>`, with a `_session` or `_weekly` suffix when the source supplies that group.
The authoritative `limits` array takes precedence over legacy fields.
Missing percentages remain unknown, and a measured zero remains zero.

For example, `remainder value --provider claude --profile default --window weekly --field remaining` selects the account weekly percentage when defined.
The usual freshness, cache, scope, account, and pace rules apply to the same normalized observation.
No model or paid pool is combined with the account allowance.

Paid usage has window ID `extra_usage` and fields `used`, `limit`, and `remaining`.
Explicit `decimal_places` permits exact conversion to `credits`; absent scale metadata preserves exact decimal values as `credits_native`.
These values are not labelled USD and are not converted to tokens or included percentages.
Missing paid values remain unknown.
When used exceeds the stated limit, both supplied values are retained and remaining is unknown.

## Scenarios

| ID | Description | Driver | Evidence |
| --- | --- | --- | --- |
| claude-collection | Profile-first identity, authoritative and legacy windows, zero, unknown, and distinct paid values are normalized through bounded HTTP. | automated `internal/claude/adapter_test.go` and `internal/claude/protocol_test.go` | `go test -race -shuffle=on -count=1 ./internal/claude` |
| claude-boundaries | Missing, expired, malformed, nonregular, redirected, oversized, canceled, wrong-account, and secret-bearing failures are bounded and redacted. | automated `internal/claude/adapter_test.go` and `internal/claude/auth_open_unix.go` | `go test -race -shuffle=on -count=1 ./internal/claude` |
| claude-cli | Compact, JSON, scalar zero, and compiled Cobra helper output preserve native CLI behavior; absent credentials are exit 1. | automated `internal/cli/claude_test.go` | `go test -race -shuffle=on -count=1 ./internal/cli` |
| claude-cache | Eligible hits avoid auth-body parsing and HTTP; 401 revokes, 403 remains transient, and 429 coalesces through existing cache policy. | automated `internal/cli/claude_test.go` | `go test -race -shuffle=on -count=1 ./internal/cli` |
| cache-unknown-identity | Fresh and stale reuse never upgrades unknown account identity. | automated `internal/cache/identity_test.go` | `go test -race -shuffle=on -count=1 ./internal/cache` |
| claude-native-canary | An authorized usable file must establish the actual source route before native certification. | manual source-specific canary | Verified 2026-09-10 against a real installed/authenticated Claude Code CLI: two live `--provider claude --profile default` observations (one cold, one `--refresh`) each returned exit 0, fresh complete evidence, `native_file_http`/`claude_credentials_json`, and the same verified account binding; a same-run cache hit reused the original `observed_at` and made zero `connect`/`socket`/`.credentials.json` `openat` calls under `strace`. Evidence retained at `.artifacts/claude-native-canary/` (git-ignored, not published). |

The [source matrix](../provider-sources.md) pins the upstream research and records the authorization boundary.
The OAuth payload contract is inferred from that pinned implementation; no published first-party schema is assumed.
Paid amounts retain the shared v1 finite numeric range while exact arithmetic preserves their decimal digits.
Controlled fixtures prove implementation behavior; the 2026-09-10 canary above is the separate live-route evidence.
