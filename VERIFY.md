# Verification

The portable verification entrypoint is `./scripts/verify.sh`.

It checks formatting, `go vet`, race-enabled shuffled tests, the reviewed Cobra dependency/import contract, and a CGO-free release-like build.

Before running offline verification from a fresh checkout or empty module cache, prime the exact versions already recorded in `go.mod` and `go.sum` once with the network-enabled setup command:

```sh
GOTOOLCHAIN=local go mod download all
```

Use the Go toolchain selected by the existing `mise.toml` and CI pin for this setup step.
After setup, the verification commands below require the populated module cache and keep `GOPROXY=off`.

The direct Go commands are:

```sh
GOPROXY=off go vet ./...
GOPROXY=off go test -race -shuffle=on -count=1 ./...
GOPROXY=off ./scripts/check-dependencies.sh
CGO_ENABLED=0 GOPROXY=off go build -trimpath -ldflags='-s -w -X main.version=v0.1.0' -o /tmp/remainder ./cmd/remainder
```

The verification command does not access credentials, a provider, a cache, or the network.

The task runner equivalent is `mise run verify`.
