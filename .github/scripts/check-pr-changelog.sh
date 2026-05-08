#!/usr/bin/env bash
# PR guard: fails when a pull request does not update the CHANGELOG.md
# "## [Unreleased]" section relative to its base ref.
#
# Required env vars:
#   GITHUB_BASE_REF  — base branch of the PR (e.g. "main")

set -euo pipefail

scriptDirectory="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=changelog.sh
source "$scriptDirectory/changelog.sh"

baseRef="${GITHUB_BASE_REF:-main}"
baseRevision="origin/$baseRef"

# Make sure the base ref is fetched.
git fetch --no-tags --depth=1 origin "$baseRef" >/dev/null 2>&1 || true

baseChangelog="$(mktemp)"
headChangelog="CHANGELOG.md"

if ! git show "$baseRevision:CHANGELOG.md" > "$baseChangelog" 2>/dev/null; then
  echo "::warning::CHANGELOG.md does not exist on $baseRevision; skipping diff check."
  : > "$baseChangelog"
fi

if [ ! -f "$headChangelog" ]; then
  echo "::error::CHANGELOG.md is missing on the PR branch."
  exit 1
fi

baseUnreleased="$(mktemp)"
headUnreleased="$(mktemp)"
extract_unreleased "$baseChangelog" > "$baseUnreleased" || true
extract_unreleased "$headChangelog" > "$headUnreleased"

if diff -q "$baseUnreleased" "$headUnreleased" >/dev/null; then
  cat <<'MESSAGE' >&2
::error::This pull request does not add an entry under `## [Unreleased]` in CHANGELOG.md.

Add a bullet under one of these subsections:
  ### Added       (new behavior; bumps minor)
  ### Changed     (behavior change; bumps minor)
  ### Removed     (removal; bumps minor)
  ### Deprecated  (deprecation; bumps minor)
  ### Fixed       (bug fix; bumps patch)
  ### Security    (security fix; bumps patch)

If this PR genuinely needs no release note (CI-only, docs typos, refactor with
no observable change), apply the `skip-changelog` label to the PR.
MESSAGE
  exit 1
fi

echo "Unreleased section was updated relative to $baseRevision."
