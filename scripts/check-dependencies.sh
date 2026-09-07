#!/bin/sh
set -eu

module=$(go list -m -f '{{.Path}}')
if [ "$module" != "github.com/douglasjarquin/remainder" ]; then
	printf 'unexpected module: %s\n' "$module" >&2
	exit 1
fi

expected_modules=$(printf '%s\n' \
	'github.com/douglasjarquin/remainder' \
	'github.com/cpuguy83/go-md2man/v2 v2.0.6' \
	'github.com/inconshreveable/mousetrap v1.1.0' \
	'github.com/russross/blackfriday/v2 v2.1.0' \
	'github.com/spf13/cobra v1.10.2' \
	'github.com/spf13/pflag v1.0.9' \
	'go.yaml.in/yaml/v3 v3.0.4' \
	'gopkg.in/check.v1 v0.0.0-20161208181325-20d25e280405')
actual_modules=$(go list -m all)
if [ "$actual_modules" != "$expected_modules" ]; then
	printf 'module graph differs from the reviewed Cobra graph\n' >&2
	printf '%s\n' "$actual_modules" >&2
	exit 1
fi

external=$(go list -deps ./... | awk '
	{
		split($0, parts, "/")
		if (parts[1] ~ /\./ && $0 !~ /^github[.]com\/douglasjarquin\/remainder($|\/)/ &&
			$0 !~ /^github[.]com\/spf13\/cobra($|\/)/ &&
			$0 !~ /^github[.]com\/spf13\/pflag($|\/)/ &&
			$0 !~ /^github[.]com\/inconshreveable\/mousetrap($|\/)/) {
			print
			found = 1
		}
	}
	END { exit found }
')
if [ -n "$external" ]; then
	printf 'unapproved imports found:\n%s\n' "$external" >&2
	exit 1
fi
