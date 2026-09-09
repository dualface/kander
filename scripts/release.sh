#!/usr/bin/env bash
# Tag a release and sync the Homebrew tap formula to it.
#
#   scripts/release.sh v0.5.0
#
# Pushing the tag is what triggers .github/workflows/release.yml; this script
# then waits for the workflow's assets and rewrites Formula/kander.rb in
# dualface/homebrew-tap from the published checksums.

set -euo pipefail

REPO_SLUG=dualface/kander
TAP_SLUG=dualface/homebrew-tap
RELEASE_BRANCH=main
# Platforms Homebrew installs from; the workflow also builds windows/*.
TAP_TARGETS=(darwin-arm64 darwin-amd64 linux-arm64 linux-amd64)
WAIT_TIMEOUT=1200
WAIT_INTERVAL=15

skip_tag=0
skip_tap=0
skip_tests=0
dry_run=0
checksums_file=

usage() {
	cat >&2 <<EOF
usage: scripts/release.sh [options] <version>

  <version>      release tag, e.g. v0.5.0

options:
  --skip-tag     the tag is already pushed; only wait and update the tap
  --skip-tap     tag and release only, leave the Homebrew formula alone
  --no-test      skip the local go vet / go test gate before tagging
  --dry-run      touch nothing: no tag, no push; print the formula instead
  --checksums F  read checksums from local file F instead of downloading them
                 (implies --dry-run; for checking the formula template)
  -h, --help     show this message
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
	--skip-tag) skip_tag=1 ;;
	--skip-tap) skip_tap=1 ;;
	--no-test) skip_tests=1 ;;
	--dry-run) dry_run=1 ;;
	--checksums)
		shift
		[ $# -gt 0 ] || die "--checksums needs a file path"
		checksums_file="$1"
		dry_run=1
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

# vMAJOR.MINOR.PATCH with an optional prerelease suffix; the formula's bare
# version is this with the leading v stripped.
if ! printf '%s' "$version" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$'; then
	die "version must look like v0.5.0, got: $version"
fi
bare_version="${version#v}"

for tool in git awk; do
	command -v "$tool" >/dev/null 2>&1 || die "missing required tool: $tool"
done
if [ -z "$checksums_file" ]; then
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

if [ -n "$checksums_file" ] && [ ! -f "$checksums_file" ]; then
	die "no such checksums file: $checksums_file"
fi

if [ "$dry_run" -eq 1 ]; then
	printf 'dry run: no tag will be created and nothing will be pushed\n' >&2
fi

if [ "$skip_tag" -eq 0 ] && [ "$dry_run" -eq 0 ]; then
	step "preflight"

	branch="$(git rev-parse --abbrev-ref HEAD)"
	[ "$branch" = "$RELEASE_BRANCH" ] || die "releases are cut from $RELEASE_BRANCH, current branch is $branch"

	[ -z "$(git status --porcelain)" ] || die "working tree is not clean; commit or stash first"

	git fetch --quiet origin "$RELEASE_BRANCH" --tags
	local_head="$(git rev-parse HEAD)"
	remote_head="$(git rev-parse "origin/$RELEASE_BRANCH")"
	[ "$local_head" = "$remote_head" ] ||
		die "HEAD ($local_head) differs from origin/$RELEASE_BRANCH ($remote_head); push or pull first"

	if git rev-parse -q --verify "refs/tags/$version" >/dev/null; then
		die "tag $version already exists locally"
	fi
	if [ -n "$(git ls-remote --tags origin "refs/tags/$version")" ]; then
		die "tag $version already exists on origin"
	fi

	if [ "$skip_tests" -eq 0 ]; then
		step "go vet ./... && go test ./..."
		command -v go >/dev/null 2>&1 || die "go not found; pass --no-test to skip the local gate"
		go vet ./...
		go test ./...
	else
		printf 'skipping local test gate (--no-test)\n' >&2
	fi

	step "tagging $version at $local_head"
	git tag -a "$version" -m "kander $bare_version"
	git push origin "$version"
	printf 'pushed tag %s; the Release workflow should start shortly\n' "$version" >&2
fi

if [ -n "$checksums_file" ]; then
	printf '\nusing local checksums: %s\n' "$checksums_file" >&2
else

step "waiting for release assets (timeout ${WAIT_TIMEOUT}s)"
deadline=$(($(date +%s) + WAIT_TIMEOUT))
while :; do
	if gh release view "$version" --repo "$REPO_SLUG" --json assets \
		--jq '[.assets[].name] | index("checksums.txt")' 2>/dev/null | grep -q '^[0-9]'; then
		printf 'release %s is published with checksums.txt\n' "$version" >&2
		break
	fi
	if [ "$(date +%s)" -ge "$deadline" ]; then
		die "timed out waiting for $version; check: gh run list --repo $REPO_SLUG"
	fi
	printf '  still building, retrying in %ss...\n' "$WAIT_INTERVAL" >&2
	sleep "$WAIT_INTERVAL"
done

fi

if [ "$skip_tap" -eq 1 ]; then
	printf '\nskipping the Homebrew tap update (--skip-tap)\n' >&2
	printf 'release %s is done: https://github.com/%s/releases/tag/%s\n' "$version" "$REPO_SLUG" "$version" >&2
	exit 0
fi

workdir="$(mktemp -d)"

if [ -n "$checksums_file" ]; then
	cp "$checksums_file" "$workdir/checksums.txt"
else
	step "reading checksums.txt from $version"
	gh release download "$version" --repo "$REPO_SLUG" --pattern checksums.txt --dir "$workdir"
fi

# checksums.txt is `sha256sum -- *` output: "<hash>  <filename>".
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
	[[ "$sha" =~ ^[0-9a-f]{64}$ ]] || die "invalid checksum for $name in checksums.txt"
	printf '%s' "$sha"
}

# Plain variables rather than an associative array, so the script still runs
# under the bash 3.2 that ships with macOS.
sha_darwin_arm64=
sha_darwin_amd64=
sha_linux_arm64=
sha_linux_amd64=
for target in "${TAP_TARGETS[@]}"; do
	sha="$(sha_for "kander-${target}.tar.gz")"
	eval "sha_$(printf '%s' "$target" | tr - _)=\$sha"
	printf '  kander-%s.tar.gz  %s\n' "$target" "$sha" >&2
done

if [ "$dry_run" -eq 1 ]; then
	mkdir -p "$workdir/tap/Formula"
else
	step "updating $TAP_SLUG"
	git clone --quiet --depth 1 "https://github.com/${TAP_SLUG}.git" "$workdir/tap"
fi

asset_url() {
	printf 'https://github.com/%s/releases/download/%s/kander-%s.tar.gz' "$REPO_SLUG" "$version" "$1"
}

cat >"$workdir/tap/Formula/kander.rb" <<EOF
class Kander < Formula
  desc "Kanban orchestration for multiple AI agents"
  homepage "https://github.com/${REPO_SLUG}"
  version "${bare_version}"
  license "MIT"

  on_macos do
    on_arm do
      url "$(asset_url darwin-arm64)"
      sha256 "${sha_darwin_arm64}"
    end
    on_intel do
      url "$(asset_url darwin-amd64)"
      sha256 "${sha_darwin_amd64}"
    end
  end

  on_linux do
    on_arm do
      url "$(asset_url linux-arm64)"
      sha256 "${sha_linux_arm64}"
    end
    on_intel do
      url "$(asset_url linux-amd64)"
      sha256 "${sha_linux_amd64}"
    end
  end

  def install
    bin.install "kander"
  end

  test do
    assert_match(/\Akander \d{8}T\d{6}Z-[0-9a-f]{12}\n\z/, shell_output("#{bin}/kander version"))
  end
end
EOF

if [ "$dry_run" -eq 1 ]; then
	step "formula that would be written to $TAP_SLUG"
	cat "$workdir/tap/Formula/kander.rb"
	exit 0
elif git -C "$workdir/tap" diff --quiet -- Formula/kander.rb; then
	printf 'formula already matches %s; nothing to push\n' "$version" >&2
else
	git -C "$workdir/tap" add Formula/kander.rb
	git -C "$workdir/tap" commit --quiet -m "kander ${bare_version}"
	git -C "$workdir/tap" push --quiet origin HEAD:main
	printf 'pushed formula for %s to %s\n' "$bare_version" "$TAP_SLUG" >&2
fi

step "done"
cat >&2 <<EOF
release:  https://github.com/${REPO_SLUG}/releases/tag/${version}
formula:  https://github.com/${TAP_SLUG}/blob/main/Formula/kander.rb
verify:   brew update && brew install ${TAP_SLUG%/*}/tap/kander
EOF
