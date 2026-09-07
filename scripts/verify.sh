#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$root"

if command -v gofumpt >/dev/null 2>&1; then
	if [ -n "$(gofumpt -l .)" ]; then
		printf 'gofumpt reported unformatted files\n' >&2
		exit 1
	fi
else
	if [ -n "$(gofmt -l .)" ]; then
		printf 'gofmt reported unformatted files\n' >&2
		exit 1
	fi
fi

GOPROXY=off go vet ./...
GOPROXY=off go test -race -shuffle=on -count=1 ./...
GOPROXY=off ./scripts/check-dependencies.sh

build_dir=$(mktemp -d)
trap 'rm -rf "$build_dir"' EXIT HUP INT TERM
CGO_ENABLED=0 GOPROXY=off go build -trimpath -ldflags='-s -w -X main.version=v0.1.0' -o "$build_dir/remainder" ./cmd/remainder
"$build_dir/remainder" --help >/dev/null
"$build_dir/remainder" --version >/dev/null
