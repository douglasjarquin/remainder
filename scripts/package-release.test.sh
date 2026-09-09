#!/bin/sh

set -eu

repository_root=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd -P)
verify_sha256sums() {
	if command -v shasum >/dev/null 2>&1; then
		shasum -a 256 -c "$1"
	elif command -v sha256sum >/dev/null 2>&1; then
		sha256sum -c "$1"
	else
		printf '%s\n' 'package-release-test: shasum or sha256sum is required' >&2
		exit 1
	fi
}
test_root=$(mktemp -d "${TMPDIR:-/tmp}/remainder-package-test.XXXXXX")
cleanup() {
	if command -v trash >/dev/null 2>&1; then
		trash "$test_root"
	elif command -v gio >/dev/null 2>&1; then
		gio trash "$test_root"
	else
		printf 'package-release-test: retained temporary directory: %s\n' "$test_root" >&2
	fi
}
trap cleanup EXIT HUP INT TERM

output_dir="$test_root/dist"
if REMAINDER_ALLOW_DIRTY=1 "$repository_root/scripts/package-release.sh" v0.1 "$output_dir" >/dev/null 2>&1; then
	printf '%s\n' 'package-release-test: malformed version was accepted' >&2
	exit 1
fi
REMAINDER_ALLOW_DIRTY=1 "$repository_root/scripts/package-release.sh" v0.1.0 "$output_dir"

archive="$output_dir/remainder_v0.1.0_darwin_arm64.tar.gz"
checksums="$output_dir/SHA256SUMS"
qa_manifest="$output_dir/remainder_v0.1.0_darwin_arm64.asset-qa.json"

test -s "$archive"
test -s "$checksums"
test -s "$qa_manifest"
(cd "$output_dir" && verify_sha256sums SHA256SUMS)

expected_files="$test_root/expected-files.txt"
actual_files="$test_root/actual-files.txt"
cat >"$expected_files" <<'EOF'
remainder_v0.1.0_darwin_arm64/
remainder_v0.1.0_darwin_arm64/ASSET_MANIFEST.json
remainder_v0.1.0_darwin_arm64/ATTRIBUTIONS.md
remainder_v0.1.0_darwin_arm64/docs/
remainder_v0.1.0_darwin_arm64/docs/provider-sources.md
remainder_v0.1.0_darwin_arm64/docs/release.md
remainder_v0.1.0_darwin_arm64/remainder
remainder_v0.1.0_darwin_arm64/skills/
remainder_v0.1.0_darwin_arm64/skills/remainder/
remainder_v0.1.0_darwin_arm64/skills/remainder/SKILL.md
remainder_v0.1.0_darwin_arm64/skills/remainder/references/
remainder_v0.1.0_darwin_arm64/skills/remainder/references/examples.md
remainder_v0.1.0_darwin_arm64/skills/remainder/references/installation.md
remainder_v0.1.0_darwin_arm64/skills/remainder/references/interpretation.md
EOF
tar -tzf "$archive" | LC_ALL=C sort >"$actual_files"
diff -u "$expected_files" "$actual_files"

extract_dir="$test_root/extract"
mkdir -p "$extract_dir"
tar -xzf "$archive" -C "$extract_dir"
binary="$extract_dir/remainder_v0.1.0_darwin_arm64/remainder"
test "$("$binary" --version)" = "remainder v0.1.0 (github.com/douglasjarquin/remainder)"
go version -m "$binary" | grep -F "path$(printf '\t')github.com/douglasjarquin/remainder/cmd/remainder" >/dev/null
grep -F '"publication_status": "blocked"' "$qa_manifest" >/dev/null
grep -F '"license_decision": "pending"' "$qa_manifest" >/dev/null
grep -F '"readiness_integration": "pending"' "$qa_manifest" >/dev/null

printf '%s\n' 'package release contract passed'
