# Examples

These examples use ordinary POSIX shell syntax and the installed executable's real Cobra commands.
Replace identifiers only with exact values obtained from the user or a Remainder observation.

```sh
remainder --help >/dev/null
remainder value --help >/dev/null

remainder --provider codex --profile default --window weekly --scope account --cache off --format compact >/dev/null
remainder --provider codex --profile default --window weekly --scope account --cache only --format json >/dev/null

remaining=$(remainder value --provider codex --profile default --window weekly --scope account --field remaining --cache only --freshness fresh)
[ "$remaining" = "42" ]

pace=$(remainder value --provider codex --profile default --window weekly --scope account --field pace --cache only --freshness fresh)
[ "$pace" = "ahead" ]

pinchos_read_codex_weekly_remaining() {
	remainder value --provider codex --profile default --window weekly --scope account --field remaining --cache only --freshness fresh
}
[ "$(pinchos_read_codex_weekly_remaining)" = "42" ]
```

The Pinchos function calls Remainder directly.
It does not require Sum or another quota intermediary.
