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
The current source also implements `claude` with profile `default`, using only its selected credentials file; its native canary remains unverified.
Check the installed executable before assuming a source increment is available.
It reads quota evidence without logging in, refreshing credentials, switching accounts, or making a generative request.
Grok and Cursor are not supported by this increment.
Missing Claude credentials are unavailable; do not use Keychain, refresh, or another credential route as a fallback.

Use exact provider, profile, window, scope, field, and optional last-observed account IDs from the user's request or a prior Remainder observation.
Do not guess an identifier or combine unlike windows.
Read [interpretation](references/interpretation.md) when choosing output, freshness, cache, or exit handling.
Read [examples](references/examples.md) for ordinary shell and direct Pinchos usage.
Read [installation](references/installation.md) only when the user asks how to install or build Remainder.

Invoking this skill must not download a package, install a runtime, overwrite a global skill, edit shell completion or profile files, access another account, or change provider authentication.

Remainder has a standard-library core and uses Cobra for its compiled command layer.
Its product direction and source research credit quota-axi; see the repository's `ATTRIBUTIONS.md` for the pinned details and licenses.
