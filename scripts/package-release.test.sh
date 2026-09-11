#!/bin/sh

set -eu

repository_root=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd -P)
case "$(uname -s):$(uname -m)" in
	Darwin:arm64)
		target=darwin_arm64
		native_goos=darwin
		native_arch=arm64
		foreign_goos=linux
		foreign_arch=amd64
		;;
	Linux:arm64 | Linux:aarch64)
		target=linux_arm64
		native_goos=linux
		native_arch=arm64
		foreign_goos=darwin
		foreign_arch=amd64
		;;
	Linux:x86_64 | Linux:amd64)
		target=linux_amd64
		native_goos=linux
		native_arch=amd64
		foreign_goos=darwin
		foreign_arch=arm64
		;;
	*)
		printf 'package-release-test: unsupported test host: %s %s\n' "$(uname -s)" "$(uname -m)" >&2
		exit 1
		;;
esac
archive_base="remainder_v0.1.0_${target}"
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
git -C "$repository_root" check-ignore -q "dist/$archive_base.tar.gz"
if "$repository_root/scripts/package-release.sh" v0.1 "$output_dir" >/dev/null 2>&1; then
	printf '%s\n' 'package-release-test: malformed version was accepted' >&2
	exit 1
fi

fake_bin="$test_root/fake-bin"
mkdir -p "$fake_bin"
cat >"$fake_bin/go" <<'EOF'
#!/bin/sh
printf '%s\n' 'go version go1.26.9 test/arm64'
EOF
chmod +x "$fake_bin/go"
if PATH="$fake_bin:$PATH" "$repository_root/scripts/package-release.sh" v0.1.0 "$output_dir" >/dev/null 2>&1; then
	printf '%s\n' 'package-release-test: wrong Go version was accepted' >&2
	exit 1
fi

dirty_repo="$test_root/dirty-repo"
git clone --quiet --no-hardlinks "$repository_root" "$dirty_repo"
git -C "$dirty_repo" checkout --quiet --detach "$(git -C "$repository_root" rev-parse HEAD)"
printf '%s\n' 'DIRTY_PROVENANCE_MARKER' >>"$dirty_repo/docs/release.md"
if REMAINDER_ALLOW_DIRTY=1 "$dirty_repo/scripts/package-release.sh" v0.1.0 "$test_root/dirty-dist" >"$test_root/dirty.stdout" 2>"$test_root/dirty.stderr"; then
	printf '%s\n' 'package-release-test: dirty source was accepted' >&2
	exit 1
fi
grep -F 'source checkout is dirty' "$test_root/dirty.stderr" >/dev/null
test ! -e "$test_root/dirty-dist"

GOOS="$foreign_goos" GOARCH="$foreign_arch" \
	"$repository_root/scripts/package-release.sh" v0.1.0 "$output_dir"

archive="$output_dir/$archive_base.tar.gz"
checksums="$output_dir/SHA256SUMS"
qa_manifest="$output_dir/$archive_base.asset-qa.json"

test -s "$archive"
test -s "$checksums"
test -s "$qa_manifest"
(cd "$output_dir" && verify_sha256sums SHA256SUMS)

expected_files="$test_root/expected-files.txt"
actual_files="$test_root/actual-files.txt"
cat >"$expected_files" <<EOF
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
if test -f "$repository_root/docs/release-readiness.md"; then
	printf '%s\n' "$archive_base/docs/release-readiness.md" >>"$expected_files"
fi
LC_ALL=C sort -o "$expected_files" "$expected_files"
tar -tzf "$archive" | LC_ALL=C sort >"$actual_files"
diff -u "$expected_files" "$actual_files"

extract_dir="$test_root/extract"
mkdir -p "$extract_dir"
tar -xzf "$archive" -C "$extract_dir"
binary="$extract_dir/$archive_base/bin/remainder"

expected_root_entries="$test_root/expected-root-entries.txt"
actual_root_entries="$test_root/actual-root-entries.txt"
printf '%s\n' ASSET_MANIFEST.json ATTRIBUTIONS.md LICENSE bin docs skills |
	LC_ALL=C sort >"$expected_root_entries"
(cd "$extract_dir/$archive_base" && ls -A) |
	LC_ALL=C sort >"$actual_root_entries"
diff -u "$expected_root_entries" "$actual_root_entries"
test -f "$binary"
test ! -e "$extract_dir/$archive_base/remainder"

cmp "$repository_root/LICENSE" "$extract_dir/$archive_base/LICENSE"
cmp "$repository_root/docs/features/claude.md" "$extract_dir/$archive_base/docs/features/claude.md"
cmp "$repository_root/docs/features/grok.md" "$extract_dir/$archive_base/docs/features/grok.md"
cmp "$repository_root/docs/features/cursor.md" "$extract_dir/$archive_base/docs/features/cursor.md"
test "$("$binary" --version)" = "remainder v0.1.0 (github.com/douglasjarquin/remainder)"
go version -m "$binary" | grep -F "path$(printf '\t')github.com/douglasjarquin/remainder/cmd/remainder" >/dev/null
go version -m "$binary" | grep -F "build$(printf '\t')GOOS=$native_goos" >/dev/null
go version -m "$binary" | grep -F "build$(printf '\t')GOARCH=$native_arch" >/dev/null
grep -F '"publication_status": "pending_final_release_verification"' "$qa_manifest" >/dev/null
grep -F '"license": "MIT"' "$qa_manifest" >/dev/null
if test -f "$repository_root/docs/release-readiness.md"; then
	grep -F '"readiness_integration": "included"' "$qa_manifest" >/dev/null
else
	grep -F '"readiness_integration": "pending"' "$qa_manifest" >/dev/null
fi

regular_source_repo="$test_root/regular-source-repo"
git clone --quiet --no-hardlinks "$repository_root" "$regular_source_repo"
git -C "$regular_source_repo" checkout --quiet --detach "$(git -C "$repository_root" rev-parse HEAD)"
regular_source_doc="$regular_source_repo/docs/features/claude.md"
regular_source_backup="$test_root/claude.md.backup"
untrusted_provider_doc="$test_root/untrusted-provider-doc.md"
printf '%s\n' 'untrusted provider doc' >"$untrusted_provider_doc"
regular_source_fake_bin="$test_root/regular-source-fake-bin"
mkdir -p "$regular_source_fake_bin"
real_go=$(command -v go)
cat >"$regular_source_fake_bin/go" <<EOF
#!/bin/sh
if test "\$1" = version; then
	mv "\$REMAINDER_PROVIDER_DOC_SOURCE" "\$REMAINDER_PROVIDER_DOC_BACKUP"
	ln -s "\$REMAINDER_UNTRUSTED_PROVIDER_DOC" "\$REMAINDER_PROVIDER_DOC_SOURCE"
fi
exec "$real_go" "\$@"
EOF
chmod +x "$regular_source_fake_bin/go"
if PATH="$regular_source_fake_bin:$PATH" \
	REMAINDER_PROVIDER_DOC_SOURCE="$regular_source_doc" \
	REMAINDER_PROVIDER_DOC_BACKUP="$regular_source_backup" \
	REMAINDER_UNTRUSTED_PROVIDER_DOC="$untrusted_provider_doc" \
	"$regular_source_repo/scripts/package-release.sh" v0.1.0 "$test_root/regular-source-dist" \
	>"$test_root/regular-source.stdout" 2>"$test_root/regular-source.stderr"; then
	printf '%s\n' 'package-release-test: symlinked required provider document source was accepted' >&2
	exit 1
fi
grep -F 'source input must be a regular file: docs/features/claude.md' "$test_root/regular-source.stderr" >/dev/null
test ! -e "$test_root/regular-source-dist"

crafted_root="$test_root/crafted"
mkdir -p "$crafted_root"
tar -xzf "$archive" -C "$crafted_root"
crafted_bundle="$crafted_root/$archive_base"
outside_marker="$test_root/outside-allowlist.txt"
printf '%s\n' 'outside bundle' >"$outside_marker"
if command -v trash >/dev/null 2>&1; then
	trash "$crafted_bundle/ATTRIBUTIONS.md"
else
	gio trash "$crafted_bundle/ATTRIBUTIONS.md"
fi
ln -s "$outside_marker" "$crafted_bundle/ATTRIBUTIONS.md"
crafted_archive="$test_root/$archive_base.tar.gz"
COPYFILE_DISABLE=1 tar -C "$crafted_root" -cf - "$archive_base" | gzip -n >"$crafted_archive"
crafted_checksums="$test_root/SHA256SUMS"
if command -v shasum >/dev/null 2>&1; then
	(cd "$test_root" && shasum -a 256 "$archive_base.tar.gz" >SHA256SUMS)
else
	(cd "$test_root" && sha256sum "$archive_base.tar.gz" >SHA256SUMS)
fi
if "$repository_root/scripts/verify-release-asset.sh" "$crafted_archive" "$crafted_checksums" \
	"$(git -C "$repository_root" rev-parse HEAD)" "$test_root/crafted-qa.json" >/dev/null 2>&1; then
	printf '%s\n' 'package-release-test: archive symlink was accepted' >&2
	exit 1
fi
test ! -e "$test_root/crafted-qa.json"

printf '%s\n' 'package release contract passed'
