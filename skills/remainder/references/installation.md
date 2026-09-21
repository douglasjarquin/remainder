# Installation

Remainder is licensed under the MIT license in the repository `LICENSE` file.
Use an approved Remainder release artifact; do not invent a download URL or claim that a source checkout is an installed release.
The candidate packaging and approved installation procedure is documented in the repository's `docs/release.md`.

Place an approved release artifact's executable in a user-selected executable directory: `bin/remainder` for v0.3.0 and later, or `remainder` at the archive root for v0.2.3 and earlier.
Copy the accompanying `skills/remainder` directory only to the user's selected Codex skill location.
Ask before replacing an existing installation.
The release executable already contains Cobra and its approved module graph, so consumers do not install Cobra or a Go toolchain.
Installing the artifact must not edit shell completion, shell profiles, provider credentials, or account state.
Keep each Remainder version in a separate directory and switch an explicit path or user-owned symlink.
Keep v0.1.0 available for Codex rollback when installing a later version.
Retain the existing collector for each provider route until its migration is separately approved.

For development from a source checkout, use the Go version pinned by `mise.toml`.
Prime the pinned module graph once with `GOTOOLCHAIN=local go mod download all`, then build with `CGO_ENABLED=0 GOPROXY=off go build -trimpath -o bin/remainder ./cmd/remainder`.
The source build uses the standard library plus the pinned Cobra graph and is not dependency-free.
Source setup is separate from consumer installation and must not run automatically when this skill is invoked.
