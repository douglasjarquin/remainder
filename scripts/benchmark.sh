#!/bin/sh
set -eu

binary=${1:-./bin/remainder}
samples=${REMAINDER_BENCH_SAMPLES:-10}

GOTOOLCHAIN=local GOPROXY=off go run ./cmd/remainder-benchmark --binary "$binary" --samples "$samples"
