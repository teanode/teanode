#!/usr/bin/env bash
# Release driver. Enumerates PRs merged since the last tag, extracts the
# ## Changelog block from each PR body, decides the bump, rewrites
# CHANGELOG.md with a dated version section, commits, and tags.
#
# Usage:
#   release.sh auto    # decides minor vs patch from merged-PR changelog blocks
#   release.sh major   # forces a major bump (still requires non-empty entries)
#
# Required env in CI:
#   GITHUB_REPOSITORY  — owner/repo, set by Actions
#   GH_TOKEN or GITHUB_TOKEN — for `gh pr view`
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

if [ -z "${GITHUB_REPOSITORY:-}" ]; then
  echo "::error::GITHUB_REPOSITORY env var is required (e.g. owner/repo)."
  exit 1
fi

currentTag="$(latest_version_tag)"
entriesFile="$(mktemp)"
collect_entries_from_prs "$currentTag" > "$entriesFile"

if [ ! -s "$entriesFile" ]; then
  echo "No changelog entries collected from merged PRs since $currentTag — nothing to release."
  emit_output released false
  exit 0
fi

if [ "$mode" = "major" ]; then
  bumpType="major"
else
  bumpType="$(decide_bump < "$entriesFile")"
fi

if [ "$bumpType" = "none" ]; then
  echo "Collected entries have no recognized sections — nothing to release."
  emit_output released false
  exit 0
fi

nextTag="$(bump_version "$currentTag" "$bumpType")"
releaseDate="$(date -u +%Y-%m-%d)"

echo "Current tag : $currentTag"
echo "Bump        : $bumpType"
echo "Next tag    : $nextTag"
echo "Release date: $releaseDate"
echo "Entries     :"
sed 's/^/  /' "$entriesFile"

if git rev-parse -q --verify "refs/tags/$nextTag" >/dev/null; then
  echo "::error::Tag $nextTag already exists; refusing to overwrite."
  exit 1
fi

sectionFile="$(mktemp)"
render_release_section "$nextTag" "$releaseDate" < "$entriesFile" > "$sectionFile"

insert_release_section CHANGELOG.md "$sectionFile"

git add CHANGELOG.md
git commit -m "chore(release): $nextTag"
git tag -a "$nextTag" -m "Release $nextTag"

emit_output released true
emit_output version "$nextTag"
