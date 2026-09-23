# CLI contract

Flags, `--all`, and exit codes. Moved out of the README.

`remainder --help` writes usage to stdout and exits successfully.

`remainder --version` writes the release or source identity to stdout and exits successfully.

An invocation without a provider selection or `--all` writes an honest unavailable message to stderr and exits nonzero.

Use `remainder --provider codex --profile default` for the selected native Codex context.
The `default` profile label means the single `CODEX_HOME` context selected by the process environment; Remainder does not scan or discover other profiles.

Unknown flags and positional commands write an actionable error to stderr and exit nonzero.

The process handles SIGINT through a context-owned interrupt path and returns the conventional 130 exit code when cancellation reaches the CLI.

Exit 0 means the selected evidence is usable, including zero, exhausted, and unlimited values.

Exit 1 means the observation is unavailable, exit 2 means invocation or selection is invalid, exit 3 means a partial observation was rendered, and exit 130 means interruption.
Selecting an unknown or non-applicable pace exits 2 with an explicit undefined-value error.

The observation records last-observed account identity separately from freshness and credential binding.

## All providers

Use `--all` to read the fixed default Codex, Claude, Grok, and Devin contexts concurrently, followed by Cursor on Linux and macOS.
The platform controls this fixed list; it does not inspect credentials to choose providers.
It cannot be combined with provider, profile, or account flags and does not discover profiles.
Mixed JSON contains separate observations and provider-scoped failures; compact output remains one line.
A provider failure preserves other usable observations with exit 3; no usable observations produces exit 1 and empty stdout.
Fresh cached observations that exceed max-age while waiting for another provider are excluded at assembly without changing their timestamps or requesting a second refresh.
