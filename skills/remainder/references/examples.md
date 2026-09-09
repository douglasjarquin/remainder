# Examples

These examples use ordinary POSIX shell syntax and the installed executable's real Cobra commands.
Replace identifiers only with exact values obtained from the user or a Remainder observation.

```sh
remainder --help
remainder value --help

remainder --provider codex --profile default --window weekly --scope account --cache auto --format compact
remainder --provider codex --profile default --window weekly --scope account --cache only --format json

remainder value --provider codex --profile default --window weekly --scope account --field remaining --cache only --freshness fresh
remainder value --provider codex --profile default --window weekly --scope account --field pace --cache only --freshness fresh

pinchos_read_codex_weekly_remaining() {
	remainder value --provider codex --profile default --window weekly --scope account --field remaining --cache only --freshness fresh
}
pinchos_read_codex_weekly_remaining
```

The Pinchos function calls Remainder directly.
It does not require Sum or another quota intermediary.
