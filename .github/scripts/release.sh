#!/usr/bin/env bash
# Release driver. Reads ## [Unreleased] from CHANGELOG.md, decides the bump,
# rewrites CHANGELOG.md with a dated version section, then commits and tags.
#
# Usage:
#   release.sh auto    # decides minor vs patch from Unreleased contents
#   release.sh major   # forces a major bump (still requires non-empty Unreleased)
#
# Required env:
#   GIT_AUTHOR_NAME, GIT_AUTHOR_EMAIL — already set by the workflow
#
# Outputs (when run inside GitHub Actions, written to $GITHUB_OUTPUT):
#   released=true|false
#   version=vX.Y.Z       (only when released=true)

set -euo pipefail

mode="${1:-}"
case "$mode" in
  auto|major) ;;
  *) echo "usage: $0 {auto|major}" >&2; exit 2 ;;
esac

scriptDirectory="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=changelog.sh
source "$scriptDirectory/changelog.sh"

repoRoot="$(git rev-parse --show-toplevel)"
cd "$repoRoot"

emit_output() {
  local key="$1"
  local value="$2"
  if [ -n "${GITHUB_OUTPUT:-}" ]; then
    printf '%s=%s\n' "$key" "$value" >> "$GITHUB_OUTPUT"
  fi
  printf '%s=%s\n' "$key" "$value"
}

unreleasedBody="$(mktemp)"
extract_unreleased CHANGELOG.md > "$unreleasedBody"

if ! grep -qE '^[[:space:]]*[-*][[:space:]]+' "$unreleasedBody"; then
  echo "No bullets under ## [Unreleased] — nothing to release."
  emit_output released false
  exit 0
fi

if [ "$mode" = "major" ]; then
  bumpType="major"
else
  bumpType="$(decide_bump < "$unreleasedBody")"
fi

if [ "$bumpType" = "none" ]; then
  echo "Unreleased section has no recognized entries — nothing to release."
  emit_output released false
  exit 0
fi

currentTag="$(latest_version_tag)"
nextTag="$(bump_version "$currentTag" "$bumpType")"
nextVersion="${nextTag#v}"
releaseDate="$(date -u +%Y-%m-%d)"

echo "Current tag : $currentTag"
echo "Bump        : $bumpType"
echo "Next tag    : $nextTag"
echo "Release date: $releaseDate"

if git rev-parse -q --verify "refs/tags/$nextTag" >/dev/null; then
  echo "::error::Tag $nextTag already exists; refusing to overwrite."
  exit 1
fi

insert_release_section CHANGELOG.md "$nextVersion" "$releaseDate"

git add CHANGELOG.md
git commit -m "chore(release): $nextTag"
git tag -a "$nextTag" -m "Release $nextTag"

emit_output released true
emit_output version "$nextTag"
