#!/bin/sh

set -eu

usage() {
	printf '%s\n' 'usage: scripts/package-release.sh VERSION [OUTPUT_DIR]' >&2
	exit 2
}

sha256_file() {
	if command -v shasum >/dev/null 2>&1; then
		shasum -a 256 "$1"
	elif command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1"
	else
		printf '%s\n' 'package-release: shasum or sha256sum is required' >&2
		exit 1
	fi
}

copy_regular_source() {
	source_path=$1
	destination_path=$2
	if test ! -f "$source_path" || test -L "$source_path"; then
		printf 'package-release: source input must be a regular file: %s\n' "$source_path" >&2
		exit 1
	fi
	cp "$source_path" "$destination_path"
}

test "$#" -ge 1 && test "$#" -le 2 || usage

version=$1
output_dir=${2:-dist}
if ! printf '%s\n' "$version" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+$'; then
	printf 'package-release: invalid version: %s\n' "$version" >&2
	exit 2
fi

repository_root=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd -P)
cd "$repository_root"

if test -n "$(git status --porcelain)" && test "${REMAINDER_ALLOW_DIRTY:-0}" != 1; then
	printf '%s\n' 'package-release: source checkout is dirty' >&2
	exit 1
fi

case "$(uname -s):$(uname -m)" in
	Darwin:arm64)
		target=darwin_arm64
		target_os=darwin
		target_arch=arm64
		;;
	Linux:arm64 | Linux:aarch64)
		target=linux_arm64
		target_os=linux
		target_arch=arm64
		;;
	*)
		printf 'package-release: unsupported build host: %s %s\n' "$(uname -s)" "$(uname -m)" >&2
		exit 1
		;;
esac

source_revision=$(git rev-parse HEAD)
source_date=$(git show -s --format=%cI HEAD)
go_version=$(GOTOOLCHAIN=local go version | awk '{print $3}')
if test "$go_version" != go1.27.1; then
	printf 'package-release: Go 1.27.1 is required; found %s\n' "$go_version" >&2
	exit 1
fi
archive_base="remainder_${version}_${target}"
archive_name="$archive_base.tar.gz"
build_root=$(mktemp -d "${TMPDIR:-/tmp}/remainder-package.XXXXXX")
cleanup() {
	if command -v trash >/dev/null 2>&1; then
		trash "$build_root"
	elif command -v gio >/dev/null 2>&1; then
		gio trash "$build_root"
	else
		printf 'package-release: retained temporary directory: %s\n' "$build_root" >&2
	fi
}
trap cleanup EXIT HUP INT TERM
bundle="$build_root/$archive_base"

mkdir -p "$bundle/docs" "$bundle/skills/remainder/references"
CGO_ENABLED=0 GOOS="$target_os" GOARCH="$target_arch" GOPROXY=off GOTOOLCHAIN=local \
	go build -buildvcs=true -trimpath \
	-ldflags="-s -w -X main.version=$version" \
	-o "$bundle/remainder" ./cmd/remainder
copy_regular_source ATTRIBUTIONS.md "$bundle/ATTRIBUTIONS.md"
copy_regular_source docs/provider-sources.md "$bundle/docs/provider-sources.md"
copy_regular_source docs/release.md "$bundle/docs/release.md"
copy_regular_source skills/remainder/SKILL.md "$bundle/skills/remainder/SKILL.md"
copy_regular_source skills/remainder/references/examples.md "$bundle/skills/remainder/references/examples.md"
copy_regular_source skills/remainder/references/installation.md "$bundle/skills/remainder/references/installation.md"
copy_regular_source skills/remainder/references/interpretation.md "$bundle/skills/remainder/references/interpretation.md"

readiness_status=pending
if test -e docs/release-readiness.md || test -L docs/release-readiness.md; then
	copy_regular_source docs/release-readiness.md "$bundle/docs/release-readiness.md"
	readiness_status=included
fi

cat >"$bundle/ASSET_MANIFEST.json" <<EOF
{
  "schema_version": "v1",
  "name": "remainder",
  "version": "$version",
  "target": "$target",
  "source_repository": "https://github.com/douglasjarquin/remainder",
  "source_revision": "$source_revision",
  "source_date": "$source_date",
  "go_version": "$go_version",
  "cgo_enabled": false,
  "license_decision": "pending",
  "readiness_document": "$readiness_status",
  "publication_status": "blocked"
}
EOF

mkdir -p "$output_dir"
output_dir=$(CDPATH='' cd -- "$output_dir" && pwd -P)
archive="$output_dir/$archive_name"
COPYFILE_DISABLE=1 tar -C "$build_root" -cf - "$archive_base" | gzip -n >"$archive"
(cd "$output_dir" && sha256_file "$archive_name" >SHA256SUMS)

"$repository_root/scripts/verify-release-asset.sh" \
	"$archive" "$output_dir/SHA256SUMS" "$source_revision" \
	"$output_dir/$archive_base.asset-qa.json"

printf '%s\n' "$archive"
