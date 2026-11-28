#!/usr/bin/env bash
# Tag a release and sync the Homebrew tap formula to it.
#
#   scripts/release.sh v2.0.0
#   scripts/release.sh v2.0.0-beta.1
#
# This branch only releases 2.x. A release (v2.Y.Z) is cut from main; a
# prerelease (v2.Y.Z-alpha|beta|rc.N) is cut from v2 and never touches the
# Homebrew tap, which carries releases only.
#
# Pushing the tag is what triggers .github/workflows/release.yml; this script
# then waits for the workflow's assets and rewrites Formula/kander.rb in
# dualface/homebrew-tap from the published checksums.
#
# The workflow now runs the tap half itself (`--skip-tag --no-test`) when the
# HOMEBREW_TAP_TOKEN secret is set, so a normal release only needs the tag. Run this
# script by hand to cut a tag locally, or to repair a formula that drifted:
#
#   scripts/release.sh --skip-tag v2.0.0

set -euo pipefail

REPO_SLUG=dualface/kander
TAP_SLUG=dualface/homebrew-tap
RELEASE_BRANCH=main
PRERELEASE_BRANCH=v2
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

  <version>      release tag v2.Y.Z (from main) or v2.Y.Z-alpha|beta|rc.N
                 (from v2, never updates the Homebrew tap)

options:
  --skip-tag     the tag is already pushed; only wait and update the tap
  --skip-tap     tag and release only, leave the Homebrew formula alone
  --no-test      skip the release gate check before tagging (the check needs a
                 successful release-gate.yml run on the commit to tag)
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

# v2.Y.Z or v2.Y.Z-(alpha|beta|rc).N, the same pattern the Release workflow
# accepts; the formula's bare version is this with the leading v stripped.
# Bash matches the whole value, as the workflow does; grep would match one
# line of a value with an embedded newline.
if ! [[ "$version" =~ ^v2\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-(alpha|beta|rc)\.(0|[1-9][0-9]*))?$ ]]; then
	die "version must look like v2.0.0 or v2.0.0-beta.1, got: $version"
fi
bare_version="${version#v}"

# A prerelease is cut from the v2 branch and never reaches the Homebrew tap.
prerelease=0
case "$version" in
*-*)
	prerelease=1
	RELEASE_BRANCH="$PRERELEASE_BRANCH"
	skip_tap=1
	;;
esac

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
	# An if keeps a false test from becoming the script's exit status.
	if [ -n "$workdir" ] && [ -d "$workdir" ]; then rm -rf "$workdir"; fi
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

	# The changelog entry must be committed before the tag: the newest
	# section of CHANGELOG.md has to be this version with a release date.
	[ -f CHANGELOG.md ] || die "CHANGELOG.md not found; add a '## $version — YYYY-MM-DD' section first"
	changelog_head="$(grep -m1 '^## ' CHANGELOG.md || true)"
	printf '%s\n' "$changelog_head" | grep -Fq "## $version — " &&
		printf '%s\n' "$changelog_head" | grep -Eq ' — [0-9]{4}-[0-9]{2}-[0-9]{2}$' ||
		if [ "$prerelease" -eq 1 ]; then
			die "the top section of CHANGELOG.md is '${changelog_head:-<none>}'; add '## $version — YYYY-MM-DD' covering every user-visible change since the previous tag, commit it on $RELEASE_BRANCH, and push $RELEASE_BRANCH"
		else
			die "the top section of CHANGELOG.md is '${changelog_head:-<none>}'; add '## $version — YYYY-MM-DD' covering every user-visible change since the previous tag, commit it on develop, and fast-forward $RELEASE_BRANCH"
		fi

	if git rev-parse -q --verify "refs/tags/$version" >/dev/null; then
		die "tag $version already exists locally"
	fi
	if [ -n "$(git ls-remote --tags origin "refs/tags/$version")" ]; then
		die "tag $version already exists on origin"
	fi

	if [ "$skip_tests" -eq 0 ]; then
		step "release gate: a successful release-gate.yml run on $local_head"
		# The full test suite runs in .github/workflows/release-gate.yml, on
		# GitHub's macOS, Windows and ubuntu runners. Only a completed,
		# successful run of that workflow on exactly this commit counts; any
		# other run, or a failed query, refuses the tag.
		gate_dispatch="gh workflow run release-gate.yml --repo $REPO_SLUG --ref $RELEASE_BRANCH (or: git push origin $local_head:refs/heads/release-gate/$version)"
		# gh prints one tab-separated line per run; awk picks the first
		# completed, successful run whose head is exactly this commit.
		gate_runs="$(gh run list --repo "$REPO_SLUG" --workflow release-gate.yml --commit "$local_head" \
			--limit 50 --json databaseId,headSha,status,conclusion \
			--jq '.[] | [.databaseId, .headSha, .status, .conclusion] | @tsv')" ||
			die "could not query the release gate runs of $local_head; no tag was created. Retry, or start the gate with: $gate_dispatch"
		gate_run="$(printf '%s\n' "$gate_runs" | awk -F '\t' -v head="$local_head" \
			'$2 == head && $3 == "completed" && $4 == "success" { print $1; exit }')"
		[ -n "$gate_run" ] ||
			die "no successful release-gate.yml run on $local_head; no tag was created. Start the gate with: $gate_dispatch, wait for it to succeed, then run this script again"
		printf 'release gate passed: https://github.com/%s/actions/runs/%s\n' "$REPO_SLUG" "$gate_run" >&2
	else
		printf 'skipping the release gate check (--no-test)\n' >&2
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
	if [ "$prerelease" -eq 1 ]; then
		printf '\nskipping the Homebrew tap update: prereleases never reach the tap\n' >&2
	else
		printf '\nskipping the Homebrew tap update (--skip-tap)\n' >&2
	fi
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
    assert_equal "kander ${bare_version}", shell_output("#{bin}/kander version").strip
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
