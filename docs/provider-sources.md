# Provider sources

Status: the selected native Remainder Codex route passed two authorized read-only observations on 2026-09-09.

This document is a feasibility matrix, not a support claim.
Fixtures and documentation establish source shape only.
They do not prove a live credential route, current login, revocation state, or provider support.

## Review basis

The pinned quota-axi source reviewed here is npm package `quota-axi@0.1.37`, with package `gitHead` `d3190237588cdf51046b27a346ff2e834855bf37`.
The single current ChatGPT-authenticated Codex route was rechecked in first-party Codex commit `283f34387b7e16bd524d8f3a431f77aa7395471d`, backend-client/rate_limit_resets.rs lines 124-128.
The pinned source does not pin the installed Codex, Claude Code, Grok, or Cursor executable versions, so an authorized lab must record each executable version alongside its observation.
The source snapshot is [quota-axi commit d319023](https://github.com/kunchenguid/quota-axi/tree/d3190237588cdf51046b27a346ff2e834855bf37), with provider implementations in [src/providers/codex.ts](https://github.com/kunchenguid/quota-axi/blob/d3190237588cdf51046b27a346ff2e834855bf37/src/providers/codex.ts), [src/providers/claude.ts](https://github.com/kunchenguid/quota-axi/blob/d3190237588cdf51046b27a346ff2e834855bf37/src/providers/claude.ts), [src/providers/grok.ts](https://github.com/kunchenguid/quota-axi/blob/d3190237588cdf51046b27a346ff2e834855bf37/src/providers/grok.ts), and [src/providers/cursor.ts](https://github.com/kunchenguid/quota-axi/blob/d3190237588cdf51046b27a346ff2e834855bf37/src/providers/cursor.ts).
The credential helpers used by the matrix are [src/providers/pi-codex-credential.ts](https://github.com/kunchenguid/quota-axi/blob/d3190237588cdf51046b27a346ff2e834855bf37/src/providers/pi-codex-credential.ts) and [src/providers/cursor-cli-credential.ts](https://github.com/kunchenguid/quota-axi/blob/d3190237588cdf51046b27a346ff2e834855bf37/src/providers/cursor-cli-credential.ts).

First-party documentation reviewed on the same date:

- [OpenAI Codex authentication](https://developers.openai.com/codex/auth/) and [Codex usage limits](https://help.openai.com/en/articles/11369540-using-codex-with-your-chatgpt-plan).
- [Claude Code authentication](https://code.claude.com/docs/en/authentication), [Claude Code statusline rate-limit fields](https://code.claude.com/docs/en/statusline), and [Claude Code installation requirements](https://code.claude.com/docs/en/installation).
- [Grok overview](https://docs.x.ai/grok/overview) and [Grok usage FAQ](https://docs.x.ai/grok/faq).
- [Cursor editor quickstart](https://prod.cursor.com/docs/get-started/quickstart), [Cursor CLI authentication](https://prod.cursor.com/docs/cli/reference/authentication), [Cursor CLI installation](https://prod.cursor.com/docs/cli/installation), and [Cursor usage and limits](https://prod.cursor.com/help/models-and-usage/usage-limits).

The current `prod.cursor.com` Cursor destinations were page-specific when reviewed.
The older `docs.cursor.com` paths redirected to the generic documentation landing page and are not used as traceability anchors here.

## Feasibility matrix

| Provider and product | Credential-source type and profile binding | Supported OS and prompt behavior | Exact read-only quota operation in pinned source | Feasibility and release status |
| --- | --- | --- | --- | --- |
| OpenAI Codex CLI using a ChatGPT subscription; executable version unpinned | OAuth access token from `$CODEX_HOME/auth.json` or the default auth file under the user's Codex directory; the pinned source also inspects the Pi `openai-codex` entry through the [Pi Codex helper](https://github.com/kunchenguid/quota-axi/blob/d3190237588cdf51046b27a346ff2e834855bf37/src/providers/pi-codex-credential.ts) and can fall back to the configured `codex` binary. Remainder implements only the selected native file context, labelled `default`, and requires its account binding; it does not treat an API key as ChatGPT quota identity. | The source has no OS-specific credential path. Remainder's direct file access is noninteractive and bounded. Remainder does not use the CLI or Pi fallbacks, login, or refresh. OpenAI documents browser sign-in for `codex login` and separately documents API-key access for usage-priced work. | Send `GET https://chatgpt.com/backend-api/wham/usage` with the OAuth bearer and `ChatGPT-Account-Id`. The current first-party Codex source uses this ChatGPT-authenticated route; Remainder does not fall back to the separate Codex API path. | **Selected native route observed twice through Remainder.** The 2026-09-09 canary returned fresh complete observations with kind `native_file_http` and name `codex_auth_json`, the same verified account binding, weekly and model constraints, reset metadata, and an explicitly unknown missing five-hour allowance. The credential file metadata remained unchanged; no login, refresh, or collector subprocess was used. |
| Anthropic Claude Code subscription | Remainder selects `$CLAUDE_CONFIG_DIR/.credentials.json` or `~/.claude/.credentials.json`, profile `default`, without discovery or fallback. An explicitly empty override is invalid. | Bounded noninteractive regular-file access on supported Unix systems, refusing symlinks and nonregular sources. No Keychain, login, refresh, or collector subprocess. | Fixed `GET https://api.anthropic.com/api/oauth/profile`, then `GET https://api.anthropic.com/api/oauth/usage`, bearer auth and beta `oauth-2025-04-20`, shared 15-second budget, redirects refused. | **Implemented with controlled fixtures; native canary unverified.** The approved default file was absent. Account identity requires the profile account UUID and includes organization UUID when supplied. See [Claude file collection](features/claude.md). |
| xAI Grok consumer and Grok CLI; executable version unpinned | Grok CLI auth from `$GROK_AUTH_JSON`, `$GROK_AUTH`, `$GROK_AUTH_PATH`, `$GROK_HOME/auth.json`, or the default Grok auth file; the pinned source also inspects a Pi `xai` OAuth/API-key entry. Bind a profile to the selected auth-file environment/path and returned account identity. | The pinned source has no consumer OS guard, while xAI's first-party usage documentation describes web/mobile Settings → Usage rather than a local credential store. File reads are noninteractive. The pinned default may delegate `grok models` to rotate the CLI session, which is not a read-only Remainder operation. | For Grok CLI OAuth, call `POST https://grok.com/grok_api_v2.GrokBuildBilling/GetGrokCreditsConfig`. If that consumer operation rejects a bearer, the source may call `GET https://cli-chat-proxy.grok.com/v1/models` or `GET https://api.x.ai/v1/models` only to distinguish model authentication from unavailable consumer quota; that probe does not provide quota. | **Unrun and not advertised.** The missing prerequisite is an authorized local Grok CLI account whose consumer quota endpoint is permitted for a read-only lab. An xAI API key alone is not evidence of consumer subscription quota. |
| Cursor editor and Cursor CLI; executable version unpinned | Editor token from Cursor `state.vscdb`, optionally overridden by `$CURSOR_STATE_DB`; CLI identity from `$CURSOR_CLI_CONFIG` or its default file, with macOS Keychain `cursor-access-token` or the Linux Cursor auth file. The pinned [Cursor CLI credential helper](https://github.com/kunchenguid/quota-axi/blob/d3190237588cdf51046b27a346ff2e834855bf37/src/providers/cursor-cli-credential.ts) owns the CLI path distinction. Bind a profile to the selected editor/CLI path and returned email or user ID. | Cursor's editor documentation covers macOS, Windows, and Linux, and the pinned editor store has separate paths for all three. The pinned CLI helper supports only macOS (`darwin`) and Linux; its Windows path is unavailable. The editor path is read without a prompt when `sqlite3` is available. macOS CLI Keychain access can prompt after the noninteractive presence check, while Linux CLI auth-file access does not prompt. The pinned source does not refresh Cursor access tokens. | `POST https://api2.cursor.sh/aiserver.v1.DashboardService/GetCurrentPeriodUsage`, plus `GetPlanInfo` and `GetSandUsageStatus`, with a bearer and empty JSON body. | **Unrun and blocked by policy for now.** The pinned source requires an external `sqlite3 -readonly` helper for the editor store, while this project has no approved SQLite/runtime exception. CLI Keychain testing also needs explicit authorization. |

## What fixtures prove

The production CLI selects the native Codex or Claude adapter only for an explicit provider and `default` profile request.
`internal/cli/fixture_test.go` provides a test-only observation adapter that records requests and call counts.
The fixture is reused by CLI contract tests and cannot be selected by the built binary.

The evidence tests use deterministic observations and local response values.
They prove parsing, normalization, rendering, selection, freshness, account binding, and exit semantics only.
They do not access credentials, Keychain items, SQLite databases, cookies, provider endpoints, or network services.

## Actual observations and release gate

One authorized external Codex feasibility canary was observed twice through pinned `quota-axi@0.1.37` using the selected existing context and `--no-credential-refresh`.
The sanitized exact evidence is recorded at `.artifacts/issue15-codex-lab/20260908T145053Z-20260908T145108Z.sanitized.jsonl`.
That earlier external result did not identify its source route and remains separate from the native evidence.

On 2026-09-09, the release-like `CGO_ENABLED=0` Remainder binary at commit `1c1c9dd1acb059c307de87d20ed02f0c83d76221` completed two authorized native observations.
Both returned exit 0, empty stderr, fresh complete evidence, source kind `native_file_http` and name `codex_auth_json`, and the same verified account fingerprint.
The response preserved weekly and model windows, durations and resets, zero credits, and an unknown absent account five-hour allowance.
The selected credential file's inode, size, and modification time were unchanged.
No login, credential refresh, generative request, account switch, or collector subprocess occurred.
The sanitized receipt is retained in the roadmap review checkout at `.artifacts/native-canary/fixed-sanitized.json`; raw credentials, account identifiers, and response bodies were not retained.

The initial native observation exposed a weekly-primary/null-secondary normalization defect.
Synthetic adapter and Cobra regressions reproduced it before the fix, and genuine duplicate supplied windows still fail closed.
The successful native observations establish this selected macOS source route; they do not certify other accounts, operating systems, or future source availability.
Claude, Grok, and Cursor have no successful native quota canary yet.
Their authorized file-only preflight is recorded below; fixture implementation does not replace source-specific live acceptance.
The issue #13 release-readiness record consolidates the already delivered cache, cross-process refresh, output, performance, and direct example evidence.
Downloaded executable installation and packaging remain the issue #14 gate.

On 2026-09-09, a separate approved documentation canary ran all seven standalone-skill commands through the release-like binary at full source SHA `bd9ca8b9928dfb993a83abb89dde840bbc146ed5`.
The compact, JSON, weekly remaining, weekly pace, and direct consumer paths passed against one selected native observation and its cache reuse.
Credential file metadata remained unchanged, and the run performed no login, credential refresh, account switch, or generative request.
That receipt remains separate from deterministic fixtures and from the earlier native feasibility canary.

Help, version, invalid input, and offline verification remain credential-free.
Expired, rejected, changed-profile, redirect, and malformed-source behavior use synthetic auth files and controlled HTTP/TLS tests.

## File-only provider preflight

On 2026-09-09, the maintainer authorized bounded read-only canaries using existing default CLI credential files.
The authorization excludes Keychain, browser cookies, SQLite, login, credential refresh, account changes, and generative calls.
The selected default Claude credential file was absent.
The selected default Grok auth file contained one consumer OIDC session for `auth.x.ai`, but that session was expired.
Neither preflight made a provider HTTP request or changed authentication state.

The additional source review uses `quota-axi@0.1.41`, tag `quota-axi-v0.1.41`, at commit `a19268827220e12e173067d11703e6ee36d5d88f`.
Its [Cursor CLI credential helper](https://github.com/kunchenguid/quota-axi/blob/a19268827220e12e173067d11703e6ee36d5d88f/src/providers/cursor-cli-credential.ts) reads the access token from Keychain on macOS and from a CLI auth file on Linux.
This reviewed implementation provides no proved macOS file-only token route.
No Cursor credential file, editor database, or Keychain item was inspected during the preflight.
These findings leave all three native quota canaries unverified and do not authorize a fallback credential route.
