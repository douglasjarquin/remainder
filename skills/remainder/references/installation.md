# Installation

No Remainder release artifact exists yet.
Do not invent a download URL or claim that a source checkout is an installed release.

When a future release publishes the standalone artifact, place its `remainder` executable in a user-selected executable directory and copy the accompanying `skills/remainder` directory only to the user's selected Codex skill location.
Ask before replacing an existing installation.
The release executable already contains Cobra and its approved module graph, so consumers do not install Cobra or a Go toolchain.
Installing the artifact must not edit shell completion, shell profiles, provider credentials, or account state.

For development from a source checkout, use the Go version pinned by `mise.toml`.
Prime the pinned module graph once with `GOTOOLCHAIN=local go mod download all`, then build with `CGO_ENABLED=0 GOPROXY=off go build -trimpath -o bin/remainder ./cmd/remainder`.
The source build uses the standard library plus the pinned Cobra graph and is not dependency-free.
Source setup is separate from consumer installation and must not run automatically when this skill is invoked.
