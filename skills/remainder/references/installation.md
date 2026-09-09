# Installation

No Remainder release artifact exists yet.
Do not invent a download URL or claim that a source checkout is an installed release.
The candidate packaging and future approved installation procedure is documented in the repository's `docs/release.md`.

When a future release publishes the standalone artifact, place its `remainder` executable in a user-selected executable directory and copy the accompanying `skills/remainder` directory only to the user's selected Codex skill location.
Ask before replacing an existing installation.
The release executable already contains Cobra and its approved module graph, so consumers do not install Cobra or a Go toolchain.
Installing the artifact must not edit shell completion, shell profiles, provider credentials, or account state.
Keep each Remainder version in a separate directory and switch an explicit path or user-owned symlink.
For the first release, retain the existing `quota-axi --provider codex --no-credential-refresh` command as the rollback path instead of inventing an earlier Remainder release.

For development from a source checkout, use the Go version pinned by `mise.toml`.
Prime the pinned module graph once with `GOTOOLCHAIN=local go mod download all`, then build with `CGO_ENABLED=0 GOPROXY=off go build -trimpath -o bin/remainder ./cmd/remainder`.
The source build uses the standard library plus the pinned Cobra graph and is not dependency-free.
Source setup is separate from consumer installation and must not run automatically when this skill is invoked.
