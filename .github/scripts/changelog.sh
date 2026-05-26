#!/usr/bin/env bash
# Shared helpers for parsing PR-description changelog blocks and computing
# release versions. Source from workflow scripts:
#
#   source .github/scripts/changelog.sh
#
# teanode's release flow: each PR description contains a `## Changelog`
# block with one or more `### <Section>` headings (Added / Changed /
# Removed / Deprecated / Fixed / Security) followed by bullets. The
# release bot reads the bodies of every PR merged since the last tag
# and composes the next release section + tag from them. PRs labeled
# `skip-changelog` contribute nothing.

set -euo pipefail

# Print the body of the ## Changelog section from a PR body file (the
# heading line and everything until the next H2 or EOF, header excluded).
# Usage: extract_pr_changelog <bodyFile>
extract_pr_changelog() {
  local path="$1"
  awk '
    /^## Changelog[[:space:]]*$/ { capture = 1; next }
    capture && /^## / { exit }
    capture { print }
  ' "$path"
}

# Read a changelog block on stdin and emit one "section|bullet" line per
# bullet found under a canonical section heading. Multi-section PRs are
# supported. The template placeholder heading
# "### Added | Changed | Removed | Deprecated | Fixed | Security" is
# ignored (it contains spaces and `|` and won't match the regex).
parse_changelog_bullets() {
  awk '
    /^### (Added|Changed|Removed|Deprecated|Fixed|Security)[[:space:]]*$/ {
      gsub(/^### |[[:space:]]+$/, "", $0)
      section = $0
      next
    }
    /^### / { section = "" }
    /^## / { section = "" }
    section != "" && /^[[:space:]]*[-*][[:space:]]+/ {
      sub(/^[[:space:]]*[-*][[:space:]]+/, "", $0)
      sub(/[[:space:]]+$/, "", $0)
      print section "|" $0
    }
  '
}

# Returns 0 if the changelog block on stdin has at least one canonical-section
# bullet that is not the TODO placeholder; returns 1 otherwise. Used by the
# PR guard.
changelog_is_filled_in() {
  local entries
  entries="$(parse_changelog_bullets)"
  if [ -z "$entries" ]; then
    return 1
  fi
  while IFS= read -r line; do
    local bullet="${line#*|}"
    if [[ "$bullet" != "TODO: "* && "$bullet" != "TODO:"* && -n "$bullet" ]]; then
      return 0
    fi
  done <<<"$entries"
  return 1
}

# Decide bump from a list of "section|bullet" entries on stdin.
# Echoes one of: major | minor | patch | none
decide_bump() {
  awk -F'|' '
    BEGIN { minor = 0; patch = 0 }
    {
      section = $1
      if (section == "Added" || section == "Changed" || section == "Removed" || section == "Deprecated") {
        minor = 1
      } else if (section == "Fixed" || section == "Security") {
        patch = 1
      }
    }
    END {
      if (minor) print "minor"
      else if (patch) print "patch"
      else print "none"
    }
  '
}

# Print the latest vX.Y.Z tag, falling back to v0.0.0 if no tags exist.
# teanode tags use the v-prefixed format (v0.1.14 etc.).
latest_version_tag() {
  local tag
  tag="$(git tag --list 'v[0-9]*.[0-9]*.[0-9]*' --sort=-v:refname | head -n 1 || true)"
  if [ -z "$tag" ]; then
    tag="v0.0.0"
  fi
  printf '%s\n' "$tag"
}

# Bump a vX.Y.Z version string by one of: major | minor | patch
# Usage: bump_version <currentTag> <bumpType>
bump_version() {
  local current="$1"
  local bump="$2"
  local stripped="${current#v}"
  local major minor patch
  IFS='.' read -r major minor patch <<<"$stripped"
  case "$bump" in
    major) major=$((major + 1)); minor=0; patch=0 ;;
    minor) minor=$((minor + 1)); patch=0 ;;
    patch) patch=$((patch + 1)) ;;
    *) echo "bump_version: unknown bump type '$bump'" >&2; return 1 ;;
  esac
  printf 'v%d.%d.%d\n' "$major" "$minor" "$patch"
}

# List PR numbers merged since the given tag, in chronological order.
# Reads from the git log: GitHub squash-merge commit subjects end with
# "(#NNN)" and merge commits start with "Merge pull request #NNN from ...".
# Empty output is a valid result (no PRs since the tag) and must not
# fail under set -e — hence the `|| true` on grep.
# Usage: prs_since_tag <tag>
prs_since_tag() {
  local tag="$1"
  local subjects
  if [ "$tag" = "v0.0.0" ]; then
    subjects="$(git log --reverse --pretty=%s)"
  else
    subjects="$(git log --reverse --pretty=%s "$tag..HEAD")"
  fi
  printf '%s\n' "$subjects" | grep -oE '#[0-9]+' | tr -d '#' | awk '!seen[$0]++' || true
}

# Collect "section|bullet" entries from every merged-PR description,
# skipping PRs labeled `skip-changelog`. Annotates each entry with the
# source PR number for traceability. Writes to stdout.
# Usage: collect_entries_from_prs <tag>
collect_entries_from_prs() {
  local tag="$1"
  local pr body label_set
  while read -r pr; do
    [ -z "$pr" ] && continue
    label_set="$(gh pr view "$pr" --repo "$GITHUB_REPOSITORY" --json labels --jq '.labels[].name' 2>/dev/null || true)"
    if grep -qFx 'skip-changelog' <<<"$label_set"; then
      printf '::notice::Skipping PR #%s (skip-changelog label).\n' "$pr" >&2
      continue
    fi
    body="$(gh pr view "$pr" --repo "$GITHUB_REPOSITORY" --json body --jq .body 2>/dev/null || true)"
    if [ -z "$body" ]; then
      printf '::warning::PR #%s has no body; skipping.\n' "$pr" >&2
      continue
    fi
    local bodyFile
    bodyFile="$(mktemp)"
    printf '%s\n' "$body" > "$bodyFile"
    local block
    block="$(extract_pr_changelog "$bodyFile")"
    if [ -z "$block" ]; then
      printf '::warning::PR #%s has no ## Changelog section; skipping.\n' "$pr" >&2
      continue
    fi
    printf '%s' "$block" | parse_changelog_bullets | sed "s/$/ (#$pr)/"
  done < <(prs_since_tag "$tag")
}

# Render a CHANGELOG.md release section from "section|bullet" entries on
# stdin. Groups by canonical section order.
# Usage: render_release_section <version> <date> < entries
render_release_section() {
  local version="$1"
  local date="$2"
  local tmp
  tmp="$(cat)"
  # Strip the leading 'v' so the heading matches Keep-a-Changelog "## [X.Y.Z]"
  # convention used by the existing Release workflow's notes extractor.
  local versionNoV="${version#v}"
  printf '## [%s] - %s\n' "$versionNoV" "$date"
  for section in Added Changed Removed Deprecated Fixed Security; do
    # shellcheck disable=SC2155
    local bullets="$(printf '%s\n' "$tmp" | awk -F'|' -v s="$section" '$1 == s { sub(/^[^|]*\|/, "", $0); print }')"
    if [ -n "$bullets" ]; then
      printf '\n### %s\n\n' "$section"
      printf '%s\n' "$bullets" | sed 's/^/- /'
    fi
  done
}

# Insert a rendered release section into CHANGELOG.md at the top (after
# the preamble, before the first existing ## heading).
# Usage: insert_release_section <changelogPath> <renderedSectionFile>
insert_release_section() {
  local path="$1"
  local sectionFile="$2"
  local tmp
  tmp="$(mktemp)"
  awk -v sectionFile="$sectionFile" '
    BEGIN { inserted = 0 }
    /^## \[/ && !inserted {
      while ((getline line < sectionFile) > 0) print line
      close(sectionFile)
      print ""
      inserted = 1
    }
    { print }
    END {
      if (!inserted) {
        while ((getline line < sectionFile) > 0) print line
      }
    }
  ' "$path" > "$tmp"
  mv "$tmp" "$path"
}
