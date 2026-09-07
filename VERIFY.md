# Verification

The portable verification entrypoint is `./scripts/verify.sh`.

It checks formatting, `go vet`, race-enabled shuffled tests, the reviewed Cobra dependency/import contract, and a CGO-free release-like build.

The direct Go commands are:

```sh
GOPROXY=off go vet ./...
GOPROXY=off go test -race -shuffle=on -count=1 ./...
GOPROXY=off ./scripts/check-dependencies.sh
CGO_ENABLED=0 GOPROXY=off go build -trimpath -ldflags='-s -w -X main.version=v0.1.0' -o /tmp/remainder ./cmd/remainder
```

The verification command does not access credentials, a provider, a cache, or the network.

The task runner equivalent is `mise run verify`.
