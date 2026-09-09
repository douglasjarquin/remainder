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
      --format string      output format: compact or json (default "compact")
      --freshness string   freshness policy: any or fresh (default "any")
  -h, --help               help for remainder
      --profile string     exact profile selection
      --provider string    exact provider selection
      --scope string       exact scope selection
  -v, --version            version for remainder
      --window string      exact window selection

Use "remainder [command] --help" for more information about a command.
`
