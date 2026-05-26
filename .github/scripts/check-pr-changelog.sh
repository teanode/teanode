#!/usr/bin/env bash
# PR guard: fails when a pull request's body does not contain a filled-in
# ## Changelog block with at least one bullet under a canonical section
# heading (Added / Changed / Removed / Deprecated / Fixed / Security).
#
# Required env vars:
#   PR_BODY  — the pull request body (multi-line string)

set -euo pipefail

scriptDirectory="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=changelog.sh
source "$scriptDirectory/changelog.sh"

bodyFile="$(mktemp)"
printf '%s\n' "${PR_BODY:-}" > "$bodyFile"

block="$(extract_pr_changelog "$bodyFile")"
if [ -z "$block" ]; then
  cat <<'MESSAGE' >&2
::error::PR body is missing the `## Changelog` section.

Open the PR description, copy the template's Changelog block (see
.github/pull_request_template.md), pick one section heading (Added /
Changed / Removed / Deprecated / Fixed / Security), and replace the
placeholder bullet.

If this PR has no user-visible change, apply the `skip-changelog` label.
MESSAGE
  exit 1
fi

if ! printf '%s' "$block" | changelog_is_filled_in; then
  cat <<'MESSAGE' >&2
::error::The PR's `## Changelog` block still contains the template placeholder.

Replace the heading line `### Added | Changed | Removed | Deprecated | Fixed | Security`
with exactly one of those section names (e.g. `### Fixed`), and replace the
`TODO: ...` bullet with a one-line user-visible summary.

If this PR has no user-visible change, apply the `skip-changelog` label.
MESSAGE
  exit 1
fi

echo "PR changelog block is filled in:"
printf '%s' "$block" | parse_changelog_bullets | sed 's/^/  /'
