#!/usr/bin/env bash
# Generate WinGet manifests for a GitHub Release and open a PR against
# microsoft/winget-pkgs.
#
#   scripts/sync-winget.sh v0.1.2
#
# The Release workflow runs this after publishing archives when WINGET_TOKEN
# is set. That secret must be a PAT that can fork microsoft/winget-pkgs and
# open a pull request (GITHUB_TOKEN cannot). Without it, repair by hand:
#
#   scripts/sync-winget.sh v0.1.2
#
# kander is a single self-contained binary with no install-time setup, so
# the manifests point at the release zips as portable nested installers.

set -euo pipefail

REPO_SLUG=dualface/kander
WINGET_UPSTREAM=microsoft/winget-pkgs
WINGET_FORK=dualface/winget-pkgs
PACKAGE_ID=Dualface.Kander
PACKAGE_PATH=manifests/d/Dualface/Kander
ZIP_AMD64=kander-windows-amd64.zip
ZIP_ARM64=kander-windows-arm64.zip
MANIFEST_VERSION=1.10.0

skip_wait=0
dry_run=0
checksums_file=
release_date=

usage() {
	cat >&2 <<EOF
usage: scripts/sync-winget.sh [options] <version>

  <version>      release tag, e.g. v0.1.2

options:
  --skip-wait      do not wait for checksums.txt on the GitHub Release
  --dry-run        print the manifests; do not fork or open a PR
  --checksums F    read checksums from local file F instead of the release
  --release-date D YYYY-MM-DD for ReleaseDate (default: the GitHub Release day)
  -h, --help       show this message
EOF
	exit 2
}

die() {
	printf 'error: %s\n' "$*" >&2
	exit 1
}

step() {
	printf '\n==> %s\n' "$*" >&2
}

version=
while [ $# -gt 0 ]; do
	case "$1" in
	--skip-wait) skip_wait=1 ;;
	--dry-run) dry_run=1 ;;
	--checksums)
		shift
		[ $# -gt 0 ] || die "--checksums needs a file path"
		checksums_file="$1"
		;;
	--release-date)
		shift
		[ $# -gt 0 ] || die "--release-date needs YYYY-MM-DD"
		release_date="$1"
		;;
	-h | --help) usage ;;
	-*) die "unknown option: $1" ;;
	*)
		[ -n "$version" ] && die "unexpected extra argument: $1"
		version="$1"
		;;
	esac
	shift
done

[ -n "$version" ] || usage

if ! printf '%s' "$version" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$'; then
	die "version must look like v0.1.2, got: $version"
fi
bare_version="${version#v}"

for tool in git awk; do
	command -v "$tool" >/dev/null 2>&1 || die "missing required tool: $tool"
done
need_gh=1
if [ -n "$checksums_file" ] && [ "$dry_run" -eq 1 ]; then
	need_gh=0
fi
if [ "$need_gh" -eq 1 ]; then
	command -v gh >/dev/null 2>&1 || die "missing required tool: gh"
	gh auth status >/dev/null 2>&1 || die "gh is not authenticated; run: gh auth login"
fi

repo_root="$(git rev-parse --show-toplevel 2>/dev/null)" || die "not inside a git repository"
cd "$repo_root"

workdir=
cleanup() {
	[ -n "$workdir" ] && [ -d "$workdir" ] && rm -rf "$workdir"
}
trap cleanup EXIT

workdir="$(mktemp -d)"

if [ -n "$checksums_file" ]; then
	[ -f "$checksums_file" ] || die "no such checksums file: $checksums_file"
	cp "$checksums_file" "$workdir/checksums.txt"
elif [ "$skip_wait" -eq 1 ]; then
	step "reading checksums.txt from $version"
	gh release download "$version" --repo "$REPO_SLUG" --pattern checksums.txt --dir "$workdir"
else
	step "waiting for release assets"
	deadline=$(($(date +%s) + 1200))
	while :; do
		if gh release view "$version" --repo "$REPO_SLUG" --json assets \
			--jq '[.assets[].name] | index("checksums.txt")' 2>/dev/null | grep -q '^[0-9]'; then
			printf 'release %s is published with checksums.txt\n' "$version" >&2
			break
		fi
		if [ "$(date +%s)" -ge "$deadline" ]; then
			die "timed out waiting for $version; check: gh run list --repo $REPO_SLUG"
		fi
		printf '  still building, retrying in 15s...\n' >&2
		sleep 15
	done
	gh release download "$version" --repo "$REPO_SLUG" --pattern checksums.txt --dir "$workdir"
fi

sha_for() {
	local name="$1" sha
	sha="$(awk -v want="$name" '
		$2 == want || $2 == "*" want {
			if (found++) exit 2
			value = $1
		}
		END {
			if (found == 1) print value
			else exit 1
		}' "$workdir/checksums.txt")" || die "expected exactly one checksum for $name in checksums.txt"
	[[ "$sha" =~ ^[0-9a-fA-F]{64}$ ]] || die "invalid checksum for $name in checksums.txt"
	printf '%s' "$sha" | tr '[:lower:]' '[:upper:]'
}

sha_amd64="$(sha_for "$ZIP_AMD64")"
sha_arm64="$(sha_for "$ZIP_ARM64")"
printf '  %s  %s\n' "$ZIP_AMD64" "$sha_amd64" "$ZIP_ARM64" "$sha_arm64" >&2

if [ -z "$release_date" ]; then
	if command -v gh >/dev/null 2>&1 && gh auth status >/dev/null 2>&1; then
		release_date="$(gh release view "$version" --repo "$REPO_SLUG" --json publishedAt --jq '.publishedAt[0:10]')" ||
			release_date=
	fi
	[ -n "$release_date" ] || release_date="$(date -u +%Y-%m-%d)"
fi
if ! printf '%s' "$release_date" | grep -Eq '^[0-9]{4}-[0-9]{2}-[0-9]{2}$'; then
	die "release date must look like 2026-09-16, got: $release_date"
fi

asset_base="https://github.com/${REPO_SLUG}/releases/download/${version}"
manifest_dir="$workdir/manifests"
mkdir -p "$manifest_dir"

cat >"$manifest_dir/${PACKAGE_ID}.yaml" <<EOF
# yaml-language-server: \$schema=https://aka.ms/winget-manifest.version.${MANIFEST_VERSION}.schema.json
PackageIdentifier: ${PACKAGE_ID}
PackageVersion: ${bare_version}
DefaultLocale: en-US
ManifestType: version
ManifestVersion: ${MANIFEST_VERSION}
EOF

cat >"$manifest_dir/${PACKAGE_ID}.installer.yaml" <<EOF
# yaml-language-server: \$schema=https://aka.ms/winget-manifest.installer.${MANIFEST_VERSION}.schema.json
PackageIdentifier: ${PACKAGE_ID}
PackageVersion: ${bare_version}
InstallerType: zip
NestedInstallerType: portable
NestedInstallerFiles:
  - RelativeFilePath: kander.exe
    PortableCommandAlias: kander
UpgradeBehavior: install
ReleaseDate: ${release_date}
Installers:
  - Architecture: x64
    InstallerUrl: ${asset_base}/${ZIP_AMD64}
    InstallerSha256: ${sha_amd64}
  - Architecture: arm64
    InstallerUrl: ${asset_base}/${ZIP_ARM64}
    InstallerSha256: ${sha_arm64}
ManifestType: installer
ManifestVersion: ${MANIFEST_VERSION}
EOF

cat >"$manifest_dir/${PACKAGE_ID}.locale.en-US.yaml" <<EOF
# yaml-language-server: \$schema=https://aka.ms/winget-manifest.defaultLocale.${MANIFEST_VERSION}.schema.json
PackageIdentifier: ${PACKAGE_ID}
PackageVersion: ${bare_version}
PackageLocale: en-US
Publisher: dualface
PublisherUrl: https://github.com/dualface
PublisherSupportUrl: https://github.com/dualface/kander/issues
Author: dualface
PackageName: Kander
PackageUrl: https://github.com/dualface/kander
License: MIT
LicenseUrl: https://github.com/dualface/kander/blob/main/LICENSE
Copyright: Copyright (c) 2026 dualface
ShortDescription: Kanban orchestration for multiple AI coding agents
Description: Kander runs rule-driven parallel development across multiple AI coding agents, with built-in independent review and delivery gates.
Moniker: kander
Tags:
  - ai
  - agent
  - cli
  - kanban
  - orchestration
ReleaseNotesUrl: https://github.com/dualface/kander/releases/tag/${version}
ManifestType: defaultLocale
ManifestVersion: ${MANIFEST_VERSION}
EOF

if [ "$dry_run" -eq 1 ]; then
	step "manifests that would be submitted to $WINGET_UPSTREAM"
	for file in "$manifest_dir"/*.yaml; do
		printf '\n----- %s -----\n' "$(basename "$file")"
		cat "$file"
	done
	exit 0
fi

step "preparing $WINGET_FORK"
if ! gh repo view "$WINGET_FORK" >/dev/null 2>&1; then
	gh repo fork "$WINGET_UPSTREAM" --clone=false --default-branch-only
fi
# A stale fork lacks earlier merged versions, which makes the PR title
# claim a new package and bases the branch on an old tree.
gh repo sync "$WINGET_FORK" --source "$WINGET_UPSTREAM"

pkgs="$workdir/winget-pkgs"
branch="${PACKAGE_ID}-${bare_version}"
cloned=0
for _ in 1 2 3 4 5 6; do
	if gh repo clone "$WINGET_FORK" "$pkgs" -- --depth 1 --filter=blob:none --sparse --quiet; then
		cloned=1
		break
	fi
	printf '  fork not ready, retrying in 5s...\n' >&2
	rm -rf "$pkgs"
	sleep 5
done
[ "$cloned" -eq 1 ] || die "could not clone $WINGET_FORK"
git -C "$pkgs" sparse-checkout set "$PACKAGE_PATH"
pr_kind="New package"
if [ -d "$pkgs/${PACKAGE_PATH}" ]; then
	pr_kind="New version"
fi
dest="$pkgs/${PACKAGE_PATH}/${bare_version}"
mkdir -p "$dest"
cp "$manifest_dir"/*.yaml "$dest/"

git -C "$pkgs" checkout -B "$branch"
git -C "$pkgs" add "${PACKAGE_PATH}/${bare_version}"
if git -C "$pkgs" diff --cached --quiet; then
	printf 'manifests already match %s; nothing to push\n' "$bare_version" >&2
	exit 0
fi

git -C "$pkgs" commit --quiet -m "${PACKAGE_ID} version ${bare_version}"
git -C "$pkgs" push --quiet --set-upstream origin "$branch"

pr_url="$(
	gh pr create --repo "$WINGET_UPSTREAM" \
		--head "dualface:${branch}" \
		--title "${pr_kind}: ${PACKAGE_ID} version ${bare_version}" \
		--body "$(
			cat <<EOF
This PR adds portable WinGet manifests for kander ${bare_version}.

- Installers: GitHub Release \`${ZIP_AMD64}\` (x64) and \`${ZIP_ARM64}\` (arm64)
- Each zip holds a single self-contained \`kander.exe\`, exposed as the \`kander\` command

Checksums: https://github.com/${REPO_SLUG}/releases/download/${version}/checksums.txt
EOF
		)"
)"

step "done"
cat >&2 <<EOF
release:   https://github.com/${REPO_SLUG}/releases/tag/${version}
manifests: ${dest}
pull:      ${pr_url}
verify:    winget install ${PACKAGE_ID}
EOF
