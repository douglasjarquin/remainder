# Claude file collection

Status: implemented with controlled file and HTTP fixtures; file-route native canary verified; macOS Keychain fallback has controlled fixtures only, native canary not yet verified.
The authorized default credential file was absent during the 2026-09-09 preflight; on 2026-09-10 the same selected `default` route was authorized and exercised against a real installed and authenticated Claude Code CLI (`~/.local/bin/claude` 2.1.267) on `host-development.douglasjarquin.i-09b74c2aaae0cbbb8` (Linux).
This source increment does not change the released v0.1.0 Codex artifact.

## Selection and identity

Use `remainder --provider claude --profile default --format json` to inspect the selected observation.
The `default` profile means the single `CLAUDE_CONFIG_DIR` context selected by the process environment, or `$HOME/.claude` when the override is unset.
An explicitly empty override does not select the current directory or a fallback.
The source tries the selected credential file first, on every operating system.
Only when that file is absent, the process is on macOS, and `--allow-keychain-prompt` was passed does it fall back to the login Keychain; see [macOS Keychain fallback](#macos-keychain-fallback).
There is no login, CLI, or refresh.

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

## macOS Keychain fallback

Claude Code on macOS stores its OAuth credentials in the login Keychain instead of writing `.credentials.json`; when that file is absent, Remainder can read the Keychain item instead of failing.
This is a fallback, not an OS branch: the file is always tried first, on every platform, exactly as above.
Only when the file is absent, `runtime.GOOS == "darwin"`, and `--allow-keychain-prompt` was passed does Remainder read the Keychain.
A cache miss with the file absent on macOS and no `--allow-keychain-prompt` fails naming the flag, without touching the Keychain.
The consent check applies only to the read itself: computing the Keychain route's cache identity (the current macOS username, no subprocess) never requires the flag, so a later invocation that omits it can still read data an earlier consented invocation already cached.
Non-darwin absent-file behavior is unchanged: today's "authentication file is missing" error, with no Keychain attempt, even when `--allow-keychain-prompt` is passed.

Use `remainder value --provider claude --profile default --window weekly --scope account --field remaining --allow-keychain-prompt` on macOS with no `.credentials.json` present.
The read is `/usr/bin/security find-generic-password -a <account> -w -s "Claude Code-credentials"`, where `<account>` is the current macOS username; no other secret is placed on the command line, output is bounded, and stderr is discarded.
The Keychain item's JSON payload is the same `claudeAiOauth` shape as `.credentials.json` and is normalized identically.
The flag authorizes a Keychain read for this invocation and may cause macOS to prompt for approval; there is no persistent consent marker.
Help, cached-only reads, and eligible cache hits never invoke the helper, even when the flag is supplied.
The file route and the Keychain route are bound to distinct cache-namespace source identities (`native_file_http`/`claude_credentials_json` and `native_keychain_http`/`claude_keychain`), so cache entries from one route never collide with the other.
There is no login, refresh, or token write on either route.

**Native canary status: not yet verified here.** This dev host is Linux and cannot access a macOS Keychain; the fallback above has controlled file, process, and adapter fixtures only (see `claude-keychain` below). A native canary of `remainder value --provider claude --profile default --window weekly --scope account --field remaining --allow-keychain-prompt` against a real macOS Keychain with no `.credentials.json` present still needs a real Mac.

## Scenarios

| ID | Description | Driver | Evidence |
| --- | --- | --- | --- |
| claude-collection | Profile-first identity, authoritative and legacy windows, zero, unknown, and distinct paid values are normalized through bounded HTTP. | automated `internal/claude/adapter_test.go` and `internal/claude/protocol_test.go` | `go test -race -shuffle=on -count=1 ./internal/claude` |
| claude-boundaries | Missing, expired, malformed, nonregular, redirected, oversized, canceled, wrong-account, and secret-bearing failures are bounded and redacted. | automated `internal/claude/adapter_test.go` and `internal/claude/auth_open_unix.go` | `go test -race -shuffle=on -count=1 ./internal/claude` |
| claude-keychain | Darwin-only fallback selection, `--allow-keychain-prompt` consent required only for an actual Keychain read (not for computing the cache identity, which never touches the helper), bounded helper execution, item-present/absent/malformed/oversized/denied fixtures, distinct cache-namespace source identity, and no helper work on cached reads or non-darwin hosts. A consented fetch followed by an unconsented `--cache=only` read of the same data succeeds without re-asking for consent. | automated `internal/claude/keychain_test.go`, `internal/claude/keychain_process_test.go`, and `internal/claude/keychain_cache_test.go` | `go test -race -shuffle=on -count=1 ./internal/claude` |
| claude-cli | Compact, JSON, scalar zero, and compiled Cobra helper output preserve native CLI behavior; absent credentials are exit 1; `--allow-keychain-prompt` is accepted for `--provider claude` and stays inert with the file absent on non-darwin hosts; `authorizeKeychainPrompt` wiring of the per-invocation flag onto the Claude and Cursor adapters is verified on every host OS, not only darwin. | automated `internal/cli/claude_test.go`, `internal/cli/claude_linux_test.go`, `internal/cli/claude_darwin_test.go`, and `internal/cli/authorize_keychain_prompt_test.go` | `go test -race -shuffle=on -count=1 ./internal/cli` |
| claude-cache | Eligible hits avoid auth-body parsing and HTTP; 401 revokes, 403 remains transient, and 429 coalesces through existing cache policy. | automated `internal/cli/claude_test.go` | `go test -race -shuffle=on -count=1 ./internal/cli` |
| cache-unknown-identity | Fresh and stale reuse never upgrades unknown account identity. | automated `internal/cache/identity_test.go` | `go test -race -shuffle=on -count=1 ./internal/cache` |
| claude-native-canary | An authorized usable file must establish the actual source route before native certification. | manual source-specific canary | Verified 2026-09-10 against a real installed/authenticated Claude Code CLI: two live `--provider claude --profile default` observations (one cold, one `--refresh`) each returned exit 0, fresh complete evidence, `native_file_http`/`claude_credentials_json`, and the same verified account binding; a same-run cache hit reused the original `observed_at` and made zero `connect`/`socket`/`.credentials.json` `openat` calls under `strace`. Evidence retained at `.artifacts/claude-native-canary/` (git-ignored, not published). |
| claude-keychain-native-canary | The macOS Keychain fallback must be exercised against a real login Keychain before native certification. | manual source-specific canary | Not yet verified here: this dev host is Linux and cannot access a macOS Keychain. A real macOS host must run `remainder value --provider claude --profile default --window weekly --scope account --field remaining --allow-keychain-prompt` with no `.credentials.json` present and record the result. |

The [source matrix](../provider-sources.md) pins the upstream research and records the authorization boundary.
The OAuth payload contract is inferred from that pinned implementation; no published first-party schema is assumed.
Paid amounts retain the shared v1 finite numeric range while exact arithmetic preserves their decimal digits.
Controlled fixtures prove implementation behavior; the 2026-09-10 canary above is the separate live-route evidence for the file route, and the Keychain fallback's native canary remains outstanding.
