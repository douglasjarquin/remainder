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
| Anthropic Claude Code subscription | Remainder selects `$CLAUDE_CONFIG_DIR/.credentials.json` or `$HOME/.claude/.credentials.json`, profile `default`, tried first on every OS. Only when that file is absent, the OS is macOS, and `--allow-keychain-prompt` is passed does Remainder fall back to the login Keychain generic password item service `Claude Code-credentials`, account the current macOS username. An explicitly empty file override is invalid; there is no other discovery or fallback. | File route: bounded noninteractive regular-file access on supported Unix systems, refusing symlinks and nonregular sources, no Keychain, login, refresh, or collector subprocess. Keychain fallback (macOS only, opt-in): bounded noninteractive `/usr/bin/security find-generic-password` read with no other secret on the command line and stderr discarded; without `--allow-keychain-prompt` a cache miss with the file absent on macOS returns unavailable naming the flag, without invoking the helper; help, cached-only reads, and eligible cache hits never invoke it. | Fixed `GET https://api.anthropic.com/api/oauth/profile`, then `GET https://api.anthropic.com/api/oauth/usage`, bearer auth and beta `oauth-2025-04-20`, shared 15-second budget, redirects refused. Identical for both the file and Keychain credential routes. | **Selected native file route observed through Remainder.** The 2026-09-10 canary returned fresh complete observations with kind `native_file_http` and name `claude_credentials_json`, the same verified account binding across a cold and a forced-refresh sample, account/five-hour/weekly/model windows, and distinct extra-usage paid credits. The credential file was read only on the two live samples; a same-run cache hit made zero network or credential-file calls. **The macOS Keychain fallback has controlled file, process, and adapter fixtures only; its native canary is not yet verified** — this dev host is Linux and cannot access a macOS Keychain. See [Claude file collection](features/claude.md). |
| xAI Grok consumer and Grok CLI; executable version unpinned | Remainder selects `GROK_AUTH_JSON`, `GROK_AUTH_PATH`, `GROK_HOME/auth.json`, otherwise `$HOME/.grok/auth.json`, profile `default`. Explicitly empty overrides and inline `GROK_AUTH` are unsupported. Local account hints do not verify identity. | Bounded noninteractive owned regular-file access on supported Unix systems, refusing symlinks and nonregular sources. No login, refresh, collector subprocess, Keychain, or browser route. | Fixed `POST https://grok.com/grok_api_v2.GrokBuildBilling/GetGrokCreditsConfig` using consumer bearer auth and an empty gRPC-web message; 15-second budget, 64 KiB response bound, redirects refused. No model-catalog or API-key fallback. | **Selected native macOS file route observed through Remainder.** The 2026-09-10 UTC canary returned fresh complete evidence after the omitted-scalar fix. Shared/product percentages and prepaid credits remain distinct, and account identity stays unknown. Immutable v0.2.0 does not contain this fix. See [Grok file collection](features/grok.md). |
| Cursor CLI; native canary records executable version | Linux: `CURSOR_CLI_CONFIG`, then `XDG_CONFIG_HOME/cursor/auth.json`, otherwise `$HOME/.config/cursor/auth.json`. macOS: `CURSOR_CLI_CONFIG`, otherwise `$HOME/.cursor/cli-config.json`, plus the fixed Cursor CLI Keychain item. Explicitly empty overrides are invalid; account identity remains unknown. | Bounded owned regular-file reads. macOS refresh requires explicit `--allow-keychain-prompt` and uses only `/usr/bin/security` for service `cursor-access-token`, account `cursor-user`; OS approval may be required. Help, default misses without consent, cached-only, and eligible hits do not invoke the helper. No consent marker, ACL change, SQLite, login, token refresh, or fallback. | Three bounded `POST` requests to `https://api2.cursor.sh/aiserver.v1.DashboardService/`: `GetCurrentPeriodUsage`, `GetPlanInfo`, and `GetSandUsageStatus`, with bearer auth and empty JSON bodies, one 15-second budget, and redirects refused. | Controlled Linux and macOS fixtures cover the selected routes. Native canary evidence must accompany the relevant release. Existing quota-axi CLI Keychain read succeeded on this host. Included percentages and spend amounts in cents remain distinct. See [Cursor CLI collection](features/cursor.md). |

## What fixtures prove

The production CLI selects the native Codex, Claude, Grok, or Cursor CLI adapter for an explicit provider and `default` profile request.
`--all` attempts Codex, Claude, and Grok default contexts, adding Cursor on Linux and macOS, with independent cache and failure handling; it performs no profile discovery.
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
On 2026-09-10, the Claude source passed two authorized live `remainder --provider claude --profile default` observations against the real installed and authenticated Claude Code CLI (`~/.local/bin/claude` 2.1.267) on `host-development.douglasjarquin.i-09b74c2aaae0cbbb8` (Linux), one cold and one `--refresh`-forced; both returned exit 0, fresh complete evidence, and the same verified account binding.
A same-run cache-hit sample, captured under `strace`, reused the first sample's `observed_at` timestamp, relabeled identity `historical`, and made zero `connect`/`socket` syscalls and zero `.credentials.json` `openat` calls.
No login, credential refresh, account switch, or generative request occurred; only the two documented read-only GETs per live sample.
Evidence is retained at `.artifacts/claude-native-canary/` in the verifying checkout; raw account identifiers are not published here.
On 2026-09-10 UTC, the Grok source at `b41972ab97a38f855439b5810a1550481b6410af` passed six actual CLI commands using the existing selected default consumer file on macOS.
Fresh JSON returned complete evidence, shared remaining 100 percent, prepaid zero credits, and behind pace; cached JSON, compact, and scalar commands retained the same observation timestamp and unknown account identity.
The credential file metadata remained unchanged, and the isolated cache was removed after verification.
No login, credential refresh, account change, or generative operation occurred.
This canary confirms the selected source route, independently of the immutable v0.2.0 release, whose decoder treats these omitted scalars as unknown.
The defaults match the [pinned quota-axi collector](https://github.com/kunchenguid/quota-axi/blob/a19268827220e12e173067d11703e6ee36d5d88f/src/providers/grok.ts#L855-L914): a valid cycle permits zero shared usage, and a present empty prepaid message represents zero credits.
The Cursor macOS CLI Keychain route succeeded through installed quota-axi 0.1.41 on 2026-09-09 with credential refresh disabled; native Remainder proof is recorded separately against its candidate binary.
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
At that preflight, all three native quota canaries remained unverified; later native results above supersede that historical status without authorizing fallback credential routes.
