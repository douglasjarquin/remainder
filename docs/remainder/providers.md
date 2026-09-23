# Providers and formats

Provider selection and output formats. Moved out of the README.

An explicit `--provider codex --profile default` selection first checks a short-lived, account/source-bound observation cache, then reads `$CODEX_HOME/auth.json` or `~/.codex/auth.json` and makes a bounded request to the Codex usage endpoint on a miss.
It does not log in, refresh credentials, switch accounts, invoke another CLI, or make a generative request.

Use `--provider claude --profile default` for the single selected `$CLAUDE_CONFIG_DIR/.credentials.json` or `~/.claude/.credentials.json` context.
On a cache miss, Claude collection verifies the account through the profile endpoint before reading quota.
It preserves session, weekly, model, and separate paid usage windows; see [Claude file collection](docs/features/claude.md) for selectors and source limits.
An absent or expired file returns unavailable; there is no Keychain or refresh fallback.

Use `--provider grok --profile default` for the selected Grok consumer file context.
Shared and product percentages remain separate from prepaid credits, and account identity stays unknown.
See [Grok file collection](docs/features/grok.md) for selectors, source limits, and mixed reports.

Use `--provider cursor --profile default` on Linux for the selected CLI auth file.
Included percentages and spend amounts in cents remain separate, with unknown account identity.
On macOS, a new Cursor observation requires `--allow-keychain-prompt`; editor SQLite remains unsupported; see [Cursor CLI collection](docs/features/cursor.md).

Use `--provider devin --profile default` for the Devin CLI credential file (`DEVIN_CREDENTIALS`, then `$XDG_DATA_HOME/devin/credentials.toml`, then `~/.local/share/devin/credentials.toml`).
On a cache miss, Devin collection makes one bounded Connect JSON `GetUserStatus` call against the credential's API server and normalizes daily and weekly quota percentages, resets, and reported ACU or credit balances.
Account identity is verified through `userStatus.userId`; see [Devin file collection](docs/features/devin.md) for selectors and source limits.

## Formats

The default report is deterministic one-line compact output.

Use `--format json` for the versioned JSON observation, `--format toon` for the same facts as a TOON document, or `value --provider PROVIDER --profile PROFILE --window WINDOW --field remaining` for one scalar value.
The scalar fields also include `reset`, `duration`, and `pace`.
