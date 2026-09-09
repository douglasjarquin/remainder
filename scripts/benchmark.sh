#!/bin/sh
set -eu

binary=${1:-./bin/remainder}
samples=${REMAINDER_BENCH_SAMPLES:-10}
if [ "$#" -gt 0 ]; then
  shift
fi

if [ -n "${REMAINDER_TOKENIZER_PYTHON:-}" ]; then
  set -- "$@" --tokenizer-python "$REMAINDER_TOKENIZER_PYTHON"
fi
if [ "${REMAINDER_BENCH_COMPARATORS:-0}" = "1" ]; then
  set -- "$@" --comparators
fi

GOTOOLCHAIN=local GOPROXY=off go run ./cmd/remainder-benchmark --binary "$binary" --samples "$samples" "$@"
