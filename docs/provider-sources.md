# Provider sources

Status: source review and the controlled Remainder Codex implementation are complete on 2026-09-08; the native Remainder endpoint canary remains unrun.

This document is a feasibility matrix, not a support claim.
Fixtures and documentation establish source shape only.
They do not prove a live credential route, current login, revocation state, or provider support.

## Review basis

The pinned quota-axi source reviewed here is npm package `quota-axi@0.1.37`, with package `gitHead` `d3190237588cdf51046b27a346ff2e834855bf37`.
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
| OpenAI Codex CLI using a ChatGPT subscription; executable version unpinned | OAuth access token from `$CODEX_HOME/auth.json` or the default auth file under the user's Codex directory; the pinned source also inspects the Pi `openai-codex` entry through the [Pi Codex helper](https://github.com/kunchenguid/quota-axi/blob/d3190237588cdf51046b27a346ff2e834855bf37/src/providers/pi-codex-credential.ts) and can fall back to the configured `codex` binary. Remainder implements only the selected native file context, labelled `default`, and requires its account binding; it does not treat an API key as ChatGPT quota identity. | The source has no OS-specific credential path. Remainder's direct file access is noninteractive and bounded. Remainder does not use the CLI or Pi fallbacks, login, or refresh. OpenAI documents browser sign-in for `codex login` and separately documents API-key access for usage-priced work. | Try `GET https://chatgpt.com/backend-api/wham/usage`, then `GET https://chatgpt.com/backend-api/codex/usage`, with the OAuth bearer and `ChatGPT-Account-Id`. | **Controlled Remainder route implemented; source-specific live support unverified.** `quota-axi@0.1.37` returned fresh read-only Codex windows twice with `--no-credential-refresh`, but its sanitized observation lacked a source discriminator. Remainder has synthetic HTTP/TLS and temp-auth coverage; its native endpoint route still needs the separately authorized canary before release support is claimed. |
| Anthropic Claude Code subscription; pinned source user-agent claude-code version 2.1.202 | OAuth session from `$CLAUDE_CONFIG_DIR/.credentials.json` or the default Claude credentials file; on macOS, the matching `Claude Code-credentials` Keychain item is profile/account scoped. Bind a Remainder profile to the resolved Claude config directory and account context. The pinned source does not use generic API-key, Bedrock, or Vertex credentials for this route. | Claude Code documents macOS, Windows, Linux, and WSL support. File reads are noninteractive. A plain macOS Keychain read is withheld until an account/profile marker exists; `--allow-keychain-prompt` can prompt. The pinned quota-axi default may delegate `claude doctor` for refresh, which is outside this task and must be disabled for a strictly read-only Remainder route. | `GET https://api.anthropic.com/api/oauth/usage` with the OAuth bearer and Claude Code beta header; `GET https://api.anthropic.com/api/oauth/profile` is used for account identity. The usage payload supplies the five-hour and seven-day windows documented by Claude Code. | **Unrun and not advertised.** A later provider issue needs an explicitly authorized account and a no-prompt or pre-granted macOS test. |
| xAI Grok consumer and Grok CLI; executable version unpinned | Grok CLI auth from `$GROK_AUTH_JSON`, `$GROK_AUTH`, `$GROK_AUTH_PATH`, `$GROK_HOME/auth.json`, or the default Grok auth file; the pinned source also inspects a Pi `xai` OAuth/API-key entry. Bind a profile to the selected auth-file environment/path and returned account identity. | The pinned source has no consumer OS guard, while xAI's first-party usage documentation describes web/mobile Settings → Usage rather than a local credential store. File reads are noninteractive. The pinned default may delegate `grok models` to rotate the CLI session, which is not a read-only Remainder operation. | For Grok CLI OAuth, call `POST https://grok.com/grok_api_v2.GrokBuildBilling/GetGrokCreditsConfig`. If that consumer operation rejects a bearer, the source may call `GET https://cli-chat-proxy.grok.com/v1/models` or `GET https://api.x.ai/v1/models` only to distinguish model authentication from unavailable consumer quota; that probe does not provide quota. | **Unrun and not advertised.** The missing prerequisite is an authorized local Grok CLI account whose consumer quota endpoint is permitted for a read-only lab. An xAI API key alone is not evidence of consumer subscription quota. |
| Cursor editor and Cursor CLI; executable version unpinned | Editor token from Cursor `state.vscdb`, optionally overridden by `$CURSOR_STATE_DB`; CLI identity from `$CURSOR_CLI_CONFIG` or its default file, with macOS Keychain `cursor-access-token` or the Linux Cursor auth file. The pinned [Cursor CLI credential helper](https://github.com/kunchenguid/quota-axi/blob/d3190237588cdf51046b27a346ff2e834855bf37/src/providers/cursor-cli-credential.ts) owns the CLI path distinction. Bind a profile to the selected editor/CLI path and returned email or user ID. | Cursor's editor documentation covers macOS, Windows, and Linux, and the pinned editor store has separate paths for all three. The pinned CLI helper supports only macOS (`darwin`) and Linux; its Windows path is unavailable. The editor path is read without a prompt when `sqlite3` is available. macOS CLI Keychain access can prompt after the noninteractive presence check, while Linux CLI auth-file access does not prompt. The pinned source does not refresh Cursor access tokens. | `POST https://api2.cursor.sh/aiserver.v1.DashboardService/GetCurrentPeriodUsage`, plus `GetPlanInfo` and `GetSandUsageStatus`, with a bearer and empty JSON body. | **Unrun and blocked by policy for now.** The pinned source requires an external `sqlite3 -readonly` helper for the editor store, while this project has no approved SQLite/runtime exception. CLI Keychain testing also needs explicit authorization. |

## What fixtures prove

The production CLI selects the native Codex adapter only for an explicit `codex` and `default` request.
`internal/cli/fixture_test.go` provides a test-only observation adapter that records requests and call counts.
The fixture is reused by CLI contract tests and cannot be selected by the built binary.

The evidence tests use deterministic observations and local response values.
They prove parsing, normalization, rendering, selection, freshness, account binding, and exit semantics only.
They do not access credentials, Keychain items, SQLite databases, cookies, provider endpoints, or network services.

## Actual observations and release gate

One authorized external Codex feasibility canary was observed twice through pinned `quota-axi@0.1.37` using the selected existing context and `--no-credential-refresh`.
The sanitized exact evidence is recorded at `.artifacts/issue15-codex-lab/20260908T145053Z-20260908T145108Z.sanitized.jsonl`.
This is not a Remainder provider observation: the native implementation is covered only by synthetic controlled sources so far.
Claude, Grok, and Cursor remain `unrun`.
No provider is advertised as live-verified from this matrix alone.

The Codex-first release remains blocked until the proven route is implemented and then observed through Remainder with the same redacted outcome, window identity, reset metadata, and non-secret account binding evidence.
The current CLI's help, version, unavailable behavior, and offline verification remain credential-free.

## Remaining live verification

The feasibility canary supplied the source prerequisite for issue5.
Now that issue5 implements the native route, the same read-only scope must be rerun through Remainder before release advertising.
Use one macOS Codex-only lab with the owner's already signed-in ChatGPT subscription and one selected `CODEX_HOME` context.

The lab should run one noninteractive read-only route with credential refresh disabled, verify that no dialog or browser opens, record only redacted status, normalized windows, reset times, and a non-secret account binding, and repeat once to distinguish stable reads from a cached artifact.
It should use temporary synthetic stores and an `httptest`-style transport for expired, revoked, locked, and changed-profile cases, with no token output, cookie import, generative request, login, refresh, account change, or cache write.
The recommendation is to keep Claude, Grok, and Cursor `unrun` until each has its own authorized account and source-specific lab, and to preserve Codex as the only release blocker.

This remains an unrun verification scope, not a live-route result.
