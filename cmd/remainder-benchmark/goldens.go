package main

const expectedHelp = `Read quota evidence from a supported provider

Usage:
  remainder [flags]
  remainder [command]

Available Commands:
  help        Help about any command
  value       Print one exact quota value

Flags:
      --account string     expected last-observed account
      --all                read all configured sources
      --cache string       cache policy: auto, off, or only (default "auto")
      --format string      output format: compact or json (default "compact")
      --freshness string   freshness policy: any or fresh (default "any")
  -h, --help               help for remainder
      --max-age duration   maximum cache observation age (default 5s)
      --profile string     exact profile selection
      --provider string    exact provider selection
      --refresh            require an observation newer than this request's starting generation
      --scope string       exact scope selection
      --stale-on-error     return stale evidence after a transient refresh failure
  -v, --version            version for remainder
      --window string      exact window selection

Use "remainder [command] --help" for more information about a command.
`
