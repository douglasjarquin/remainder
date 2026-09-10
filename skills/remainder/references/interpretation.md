# Interpretation

Treat the executable's help and returned observation as the command and identifier catalog.
The source supports providers `codex`, `claude`, `grok`, and `cursor` with profile `default`.
Release v0.2.1 includes Codex, macOS Cursor CLI Keychain, and the selected macOS Grok consumer file route.
Claude and Linux Cursor source fixtures do not certify native access.
Grok uses `credits`, `product:<kind>`, and `prepaid` windows and cannot verify an explicit account selector.
Cursor uses `included_usage`, `auto_usage`, `api_usage`, `spend_limit`, and `grok_bot` when the source provides them.
Its spend-limit amounts use `usd_cents`, separate from percentage allowances, and its unknown identity cannot satisfy an explicit account selector.
Window IDs such as `five_hour` and `weekly`, scopes such as `account`, `model`, and `global`, and field IDs such as `remaining`, `reset`, `duration`, and `pace` are exact values rather than display labels.
An `--account` value is an assertion about the observation's last-observed account identity, not an instruction to switch accounts.

The default compact format is a one-line summary.
Use `--format json` when a caller needs the full typed observation.
Use `remainder value` for one scalar and provide exact `--provider`, `--profile`, `--window`, and `--field` values; add `--scope` when a window ID alone would be ambiguous.

`--freshness any` accepts fresh, stale, or unknown freshness, while `--freshness fresh` rejects anything other than fresh evidence.
The default `--cache auto --max-age 5s` policy may reuse an eligible complete observation.
`--cache off` performs a bounded source read.
`--cache only` refuses a miss without a provider request or credential parsing.
`--refresh` requires a generation newer than the request's starting generation and cannot be combined with cached-only mode.
`--stale-on-error` permits expired evidence only after a transient refresh failure.

`observed_at` is the original observation time and remains unchanged when cached evidence is rendered again.
Compact `age_seconds` is measured from that original observation.
`source_at`, when present, is the provider's source timestamp rather than a cache-write time.
A historical account binding describes the last observed identity and does not prove the current login.
Unknown identity remains unknown after cache reuse.

`--all` attempts Codex, Claude, and Grok default contexts, plus Cursor on Linux and macOS, preserving separate observations and failures in a mixed report.
Do not pass provider, profile, or account flags with it.
A failed source does not discard usable evidence from another source; preserve partial output on exit 3.
A fresh cache hit that exceeds max-age while another source runs is excluded without changing its observation timestamp.

Exit 0 means usable selected evidence, including an explicit zero or unlimited allowance.
Zero means exhausted and is distinct from `unknown` or `not_applicable`.
Exit 1 means evidence is unavailable.
Exit 2 means the invocation or selection is invalid, including an undefined scalar.
Exit 3 means partial evidence was rendered, so preserve the output and surface the partial status.
Exit 130 means interruption.

Do not turn unknown values into zero, select a minimum across incomparable pools, or treat credits as included allowance.
Pace is per percentage window and can be `ahead`, `on_pace`, `behind`, or `unknown`; use the recorded reason when it is unknown.
