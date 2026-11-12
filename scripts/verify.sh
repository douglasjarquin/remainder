#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$root"

formatter=gofmt
if command -v gofumpt >/dev/null 2>&1 && gofumpt --version >/dev/null 2>&1; then
	formatter=gofumpt
fi
unformatted=$("$formatter" -l .)
if [ -n "$unformatted" ]; then
	printf '%s reported unformatted files:\n%s\n' "$formatter" "$unformatted" >&2
	exit 1
fi

GOPROXY=off go vet ./...
GOPROXY=off go test -race -shuffle=on -count=1 ./...
GOPROXY=off ./scripts/check-dependencies.sh
PYTHONDONTWRITEBYTECODE=1 mise exec python@3.13.5 -- python .agents/skills/evidence/scripts/test_redaction.py

build_dir=$(mktemp -d)
trap 'rm -rf "$build_dir"' EXIT HUP INT TERM
CGO_ENABLED=0 GOPROXY=off go build -trimpath -ldflags='-s -w -X main.version=v0.1.0' -o "$build_dir/remainder" ./cmd/remainder
"$build_dir/remainder" --help >/dev/null
"$build_dir/remainder" --version >/dev/null
