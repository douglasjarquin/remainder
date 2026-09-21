#!/usr/bin/env bash
# Install the remainder CLI from published GitHub Releases archives.
#
#   curl -fsSL https://raw.githubusercontent.com/douglasjarquin/remainder/main/scripts/install.sh | bash
#
# Environment overrides:
#   REMAINDER_VERSION      release tag to install, with or without the leading
#                          v (default: the newest release shipping an archive
#                          for this platform; releases are tagged per platform,
#                          so that is not always the newest tag overall)
#   REMAINDER_INSTALL_DIR  install directory (default: ~/.local/bin)

set -euo pipefail

repo=douglasjarquin/remainder
install_dir=${REMAINDER_INSTALL_DIR:-"$HOME/.local/bin"}

fail() {
	printf 'install: %s\n' "$*" >&2
	exit 1
}

case "$(uname -s):$(uname -m)" in
	Darwin:arm64) target=darwin_arm64 ;;
	Darwin:x86_64) fail 'no darwin_amd64 archive is published' ;;
	Linux:arm64 | Linux:aarch64) target=linux_arm64 ;;
	Linux:x86_64 | Linux:amd64) target=linux_amd64 ;;
	*) fail "unsupported platform: $(uname -s) $(uname -m)" ;;
esac

if test -n "${REMAINDER_VERSION:-}"; then
	version=${REMAINDER_VERSION#v}
	printf '%s' "$version" | grep -qE '^[0-9]+\.[0-9]+\.[0-9]+$' ||
		fail "invalid REMAINDER_VERSION: $REMAINDER_VERSION"
	tag=v$version
else
	printf 'install: resolving newest %s release\n' "$target" >&2
	releases=$(curl -fsSL "https://api.github.com/repos/$repo/releases?per_page=30") ||
		fail 'cannot list GitHub releases; set REMAINDER_VERSION to select a tag directly'
	asset_url=$(printf '%s\n' "$releases" |
		grep -oE "https://github.com/$repo/releases/download/v[0-9]+\.[0-9]+\.[0-9]+/remainder_v[0-9]+\.[0-9]+\.[0-9]+_${target}\.tar\.gz" |
		head -n 1 || true)
	test -n "$asset_url" || fail "no published release ships a $target archive"
	tag=$(basename "$(dirname "$asset_url")")
fi

archive="remainder_${tag}_${target}.tar.gz"
release_url="https://github.com/$repo/releases/download/$tag"

tmp_dir=$(mktemp -d "${TMPDIR:-/tmp}/remainder-install.XXXXXX")
cleanup() {
	rm -rf "$tmp_dir"
}
trap cleanup EXIT HUP INT TERM

printf 'install: downloading %s\n' "$archive" >&2
curl -fsSL -o "$tmp_dir/$archive" "$release_url/$archive" ||
	fail "cannot download $archive from $tag"
curl -fsSL -o "$tmp_dir/SHA256SUMS" "$release_url/SHA256SUMS" ||
	fail "cannot download SHA256SUMS from $tag"

awk -v name="$archive" '$2 == name { print; count++ } END { if (count != 1) exit 1 }' \
	"$tmp_dir/SHA256SUMS" >"$tmp_dir/checksum.txt" ||
	fail "SHA256SUMS in $tag has no unique entry for $archive"
if command -v shasum >/dev/null 2>&1; then
	(cd "$tmp_dir" && shasum -a 256 -c checksum.txt)
elif command -v sha256sum >/dev/null 2>&1; then
	(cd "$tmp_dir" && sha256sum -c checksum.txt)
else
	fail 'shasum or sha256sum is required'
fi

tar -xzf "$tmp_dir/$archive" -C "$tmp_dir"
bundle="$tmp_dir/remainder_${tag}_${target}"
if test -f "$bundle/bin/remainder"; then
	binary=$bundle/bin/remainder
elif test -f "$bundle/remainder"; then
	binary=$bundle/remainder
else
	fail "archive lacks a remainder binary under $bundle"
fi

installed_version=$("$binary" --version) || fail 'downloaded binary failed --version'
test "$installed_version" = "remainder $tag (github.com/$repo)" ||
	fail "unexpected version output: $installed_version"

mkdir -p "$install_dir"
install -m 0755 "$binary" "$install_dir/remainder" ||
	fail "cannot write $install_dir/remainder; set REMAINDER_INSTALL_DIR to a writable directory"

printf 'installed %s to %s/remainder\n' "$installed_version" "$install_dir"
case ":$PATH:" in
	*":$install_dir:"*) ;;
	*) printf 'install: %s is not on PATH; add it or run %s/remainder directly\n' "$install_dir" "$install_dir" ;;
esac
printf 'next: remainder --help\n'
