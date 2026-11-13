---
title: Build and verify Remainder only with the mise-pinned Go toolchain and its approved dependency set
applies_when:
  - adding or changing Go code, build flags, or module dependencies
  - running builds, tests, or verification for this repository
tags: [go, mise, toolchain, dependencies]
---

Use Go 1.27.4, the canonical developer and CI toolchain pin selected by `mise.toml`.
Drive work through the mise tasks: `mise run build` produces `bin/remainder` with `CGO_ENABLED=0`, `mise run test` runs race-enabled shuffled tests, and `mise run verify` runs `scripts/verify.sh`.
Runtime and ordinary tests use only the Go standard library plus the pinned Cobra and go-toon modules; do not add dependencies.
Release-like builds use `CGO_ENABLED=0`.
Prime the pinned module cache once before offline verification with `GOTOOLCHAIN=local go mod download all`; after that, verification commands keep `GOPROXY=off`.
