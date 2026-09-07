#!/bin/sh
set -eu

binary=${1:-./bin/remainder}
samples=${REMAINDER_BENCH_SAMPLES:-10}
if [ ! -x "$binary" ]; then
	printf 'benchmark binary is not executable: %s\n' "$binary" >&2
	exit 1
fi

case "$samples" in
	''|*[!0-9]*) printf 'REMAINDER_BENCH_SAMPLES must be an integer\n' >&2; exit 1 ;;
esac
if [ "$samples" -lt 1 ]; then
	printf 'REMAINDER_BENCH_SAMPLES must be at least 1\n' >&2
	exit 1
fi

temp_dir=$(mktemp -d)
trap 'rm -rf "$temp_dir"' EXIT HUP INT TERM

version=$("$binary" --version)
machine=$(uname -smr)
go_version=$(go version)
size_bytes=$(wc -c < "$binary" | tr -d ' ')
printf '{"kind":"metadata","source":"full-process-cobra-entrypoint","binary":"%s","version":"%s","flags":["--help"],"machine":"%s","go":"%s","size_bytes":%s,"samples":%s}\n' "$binary" "$version" "$machine" "$go_version" "$size_bytes" "$samples"

i=1
while [ "$i" -le "$samples" ]; do
	start_ns=$(date +%s%N)
	"$binary" --help >"$temp_dir/stdout" 2>"$temp_dir/stderr"
	code=$?
	end_ns=$(date +%s%N)
	if [ "$code" -ne 0 ] || [ -s "$temp_dir/stderr" ]; then
		printf 'benchmark invocation failed at sample %s\n' "$i" >&2
		exit 1
	fi
	elapsed_ns=$((end_ns - start_ns))
	printf '{"kind":"sample","sample":%s,"elapsed_ns":%s}\n' "$i" "$elapsed_ns"
	i=$((i + 1))
done
