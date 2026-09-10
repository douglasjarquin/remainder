---
name: remainder
description: Read and interpret quota evidence with the standalone Remainder CLI when a user asks about provider limits, remaining quota, reset timing, or spending pace.
---

# Remainder

Use the installed `remainder` executable as the source of truth for commands, flags, and current provider behavior.
Start with `remainder --help`, and use `remainder value --help` before selecting one scalar.

If the executable is absent, give one actionable diagnostic: `remainder is not installed; install a Remainder release artifact or build it from source.`
Stop after that diagnostic unless the user asks to install or build it.

The v0.1.0 release supports provider `codex` with profile `default`.
The v0.2.0 release scope adds the verified macOS Cursor CLI Keychain route with profile `default`.
Claude, Grok, and Linux Cursor source implementations have controlled fixtures but remain outside native release certification.
Retain the existing collector for an unverified route.
Check the installed executable before assuming a source increment is available.
It reads quota evidence without logging in, refreshing credentials, switching accounts, or making a generative request.
Cursor uses the Linux CLI auth file or, on macOS, the CLI Keychain item with explicit `--allow-keychain-prompt` for refreshes.
The flag may cause an OS approval dialog; default misses, help, cached-only reads, and eligible hits never invoke Keychain.
Editor SQLite and token refresh remain unsupported.
Use `--all` for the fixed provider default contexts only when the installed executable supports it; retain separate observations and provider failures.
Missing or expired credentials are unavailable; do not use Keychain, refresh, or another credential route as a fallback.

Use exact provider, profile, window, scope, field, and optional last-observed account IDs from the user's request or a prior Remainder observation.
Do not guess an identifier or combine unlike windows.
Read [interpretation](references/interpretation.md) when choosing output, freshness, cache, or exit handling.
Read [examples](references/examples.md) for ordinary shell and direct Pinchos usage.
Read [installation](references/installation.md) only when the user asks how to install or build Remainder.

Invoking this skill must not download a package, install a runtime, overwrite a global skill, edit shell completion or profile files, access another account, or change provider authentication.

Remainder has a standard-library core and uses Cobra for its compiled command layer.
Its product direction and source research credit quota-axi; see the repository's `ATTRIBUTIONS.md` for the pinned details and licenses.
