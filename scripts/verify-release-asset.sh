#!/bin/sh

set -eu

sha256_file() {
	if command -v shasum >/dev/null 2>&1; then
		shasum -a 256 "$1"
	elif command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1"
	else
		printf '%s\n' 'verify-release-asset: shasum or sha256sum is required' >&2
		exit 1
	fi
}

if test "$#" -ne 4; then
	printf '%s\n' 'usage: scripts/verify-release-asset.sh ARCHIVE SHA256SUMS SOURCE_REVISION OUTPUT_MANIFEST' >&2
	exit 2
fi

archive=$1
checksums=$2
source_revision=$3
output_manifest=$4
archive_name=$(basename -- "$archive")
archive_base=${archive_name%.tar.gz}
case "$archive_base" in
	remainder_v[0-9]*.[0-9]*.[0-9]*_darwin_arm64)
		target=darwin_arm64
		expected_goos=darwin
		expected_goarch=arm64
		;;
	remainder_v[0-9]*.[0-9]*.[0-9]*_linux_arm64)
		target=linux_arm64
		expected_goos=linux
		expected_goarch=arm64
		;;
	remainder_v[0-9]*.[0-9]*.[0-9]*_linux_amd64)
		target=linux_amd64
		expected_goos=linux
		expected_goarch=amd64
		;;
	*)
		printf 'verify-release-asset: unsupported archive name: %s\n' "$archive_name" >&2
		exit 2
		;;
esac
version=${archive_base#remainder_}
version=${version%_"$target"}

expected_sha256=$(awk -v name="$archive_name" '$2 == name { print $1 }' "$checksums")
test -n "$expected_sha256"
actual_sha256=$(sha256_file "$archive" | awk '{print $1}')
test "$actual_sha256" = "$expected_sha256"
printf '%s: OK\n' "$archive_name"

qa_root=$(mktemp -d "${TMPDIR:-/tmp}/remainder-asset-qa.XXXXXX")
cleanup() {
	if command -v trash >/dev/null 2>&1; then
		trash "$qa_root"
	elif command -v gio >/dev/null 2>&1; then
		gio trash "$qa_root"
	else
		printf 'verify-release-asset: retained temporary directory: %s\n' "$qa_root" >&2
	fi
}
trap cleanup EXIT HUP INT TERM
listing="$qa_root/archive-files.txt"
expected="$qa_root/expected-files.txt"
tar -tzf "$archive" | LC_ALL=C sort >"$listing"

if ! tar -tvzf "$archive" | awk '$1 !~ /^[-d]/ { exit 1 }'; then
	printf '%s\n' 'verify-release-asset: archive contains a non-regular file or non-directory entry' >&2
	exit 1
fi

cat >"$expected" <<EOF
$archive_base/
$archive_base/ASSET_MANIFEST.json
$archive_base/ATTRIBUTIONS.md
$archive_base/LICENSE
$archive_base/docs/
$archive_base/docs/features/
$archive_base/docs/features/claude.md
$archive_base/docs/features/cursor.md
$archive_base/docs/features/grok.md
$archive_base/docs/provider-sources.md
$archive_base/docs/release.md
$archive_base/bin/
$archive_base/bin/remainder
$archive_base/skills/
$archive_base/skills/remainder/
$archive_base/skills/remainder/SKILL.md
$archive_base/skills/remainder/references/
$archive_base/skills/remainder/references/examples.md
$archive_base/skills/remainder/references/installation.md
$archive_base/skills/remainder/references/interpretation.md
EOF
if tar -tzf "$archive" | grep -Eq '(^/|(^|/)\.\.(/|$))'; then
	printf '%s\n' 'verify-release-asset: unsafe archive path' >&2
	exit 1
fi
if tar -tzf "$archive" | grep -Fx "$archive_base/docs/release-readiness.md" >/dev/null; then
	printf '%s\n' "$archive_base/docs/release-readiness.md" >>"$expected"
fi
LC_ALL=C sort -o "$expected" "$expected"
diff -u "$expected" "$listing"

tar -xzf "$archive" -C "$qa_root"
bundle="$qa_root/$archive_base"
binary="$bundle/bin/remainder"
cmp "docs/features/claude.md" "$bundle/docs/features/claude.md"
cmp "docs/features/grok.md" "$bundle/docs/features/grok.md"
cmp "docs/features/cursor.md" "$bundle/docs/features/cursor.md"
runtime_home="$qa_root/runtime-home"
runtime_path="$qa_root/runtime-path"
mkdir -p "$runtime_home" "$runtime_path"

run_clean() {
	stdout=$1
	stderr=$2
	shift 2
	env -i HOME="$runtime_home" CODEX_HOME="$runtime_home/codex" \
		XDG_CACHE_HOME="$runtime_home/cache" PATH="$runtime_path" \
		"$binary" "$@" >"$stdout" 2>"$stderr"
}

run_clean "$qa_root/version.stdout" "$qa_root/version.stderr" --version
test ! -s "$qa_root/version.stderr"
test "$(cat "$qa_root/version.stdout")" = "remainder $version (github.com/douglasjarquin/remainder)"

run_clean "$qa_root/root-help.stdout" "$qa_root/root-help.stderr" --help
test ! -s "$qa_root/root-help.stderr"
grep -F 'Usage:' "$qa_root/root-help.stdout" >/dev/null

run_clean "$qa_root/subcommand-help.stdout" "$qa_root/subcommand-help.stderr" value --help
test ! -s "$qa_root/subcommand-help.stderr"
grep -F 'Print one exact quota value' "$qa_root/subcommand-help.stdout" >/dev/null

set +e
run_clean "$qa_root/error.stdout" "$qa_root/error.stderr" --definitely-invalid
error_exit=$?
set -e
test "$error_exit" -eq 2
test ! -s "$qa_root/error.stdout"
grep -F 'unknown flag: --definitely-invalid' "$qa_root/error.stderr" >/dev/null
if grep -F 'Usage:' "$qa_root/error.stderr" >/dev/null; then
	printf '%s\n' 'verify-release-asset: operational error appended usage' >&2
	exit 1
fi

build_metadata="$qa_root/build-metadata.txt"
go version -m "$binary" >"$build_metadata"
grep -F "vcs.revision=$source_revision" "$build_metadata" >/dev/null
grep -F "path$(printf '\t')github.com/douglasjarquin/remainder/cmd/remainder" "$build_metadata" >/dev/null
grep -F ': go1.27.1' "$build_metadata" >/dev/null
grep -F "build$(printf '\t')CGO_ENABLED=0" "$build_metadata" >/dev/null
grep -F "build$(printf '\t')GOOS=$expected_goos" "$build_metadata" >/dev/null
grep -F "build$(printf '\t')GOARCH=$expected_goarch" "$build_metadata" >/dev/null
grep -F "build$(printf '\t')vcs.modified=false" "$build_metadata" >/dev/null
grep -F "\"source_revision\": \"$source_revision\"" "$bundle/ASSET_MANIFEST.json" >/dev/null
grep -F "\"version\": \"$version\"" "$bundle/ASSET_MANIFEST.json" >/dev/null
grep -F "\"target\": \"$target\"" "$bundle/ASSET_MANIFEST.json" >/dev/null
grep -F '"go_version": "go1.27.1"' "$bundle/ASSET_MANIFEST.json" >/dev/null
grep -F '"cgo_enabled": false' "$bundle/ASSET_MANIFEST.json" >/dev/null

readiness_status=pending
if test -f "$bundle/docs/release-readiness.md"; then
	readiness_status=included
fi
archive_sha256=$actual_sha256
source_path=$(pwd -P | sed 's/\\/\\\\/g; s/"/\\"/g')
cat >"$output_manifest" <<EOF
{
  "schema_version": "v1",
  "asset": "$archive_name",
  "sha256": "$archive_sha256",
  "version": "$version",
  "target": "$target",
  "source_revision": "$source_revision",
  "source_path": "$source_path",
  "checks": {
    "checksum": "passed",
    "archive_path_safety": "passed",
    "file_allowlist": "passed",
    "clean_runtime_path": "passed",
    "root_help": "passed",
    "subcommand_help": "passed",
    "error_exit_and_no_usage_dump": "passed",
    "version_output": "passed",
    "embedded_source_revision": "passed",
    "embedded_target": "passed",
    "embedded_go_version": "passed",
    "embedded_cgo_disabled": "passed",
    "embedded_clean_source": "passed",
    "asset_manifest": "passed",
    "provider_document_bytes": "passed"
  },
  "runtime_path_excluded": ["go", "node", "python", "jq", "sum", "herdr"],
  "license": "MIT",
  "readiness_integration": "$readiness_status",
  "publication_status": "pending_final_release_verification",
  "limitations": [
    "This is a local pre-publication candidate, not a GitHub download or public release.",
    "The MIT license is adopted; publication is pending final release verification.",
    "The root session must independently verify the integrated issue 13 readiness on the final release candidate before publication.",
    "Packaged data outputs, cache faults, interruption, coexistence, startup timing, and the authorized live Codex canary remain final root-owned release gates.",
    "No live provider canary, global installation, mise GitHub install, upload, tag, or release was performed."
  ]
}
EOF

printf '%s\n' "$output_manifest"
