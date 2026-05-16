#!/usr/bin/env bash
# Shared helpers for parsing CHANGELOG.md and computing release versions.
# Source this file from workflow scripts: `source .github/scripts/changelog.sh`

set -euo pipefail

# Print the body of the ## [Unreleased] section from a changelog file (header excluded).
# Usage: extract_unreleased <path>
extract_unreleased() {
  local path="$1"
  awk '
    /^## \[Unreleased\][[:space:]]*$/ { capture = 1; next }
    capture && /^## \[/ { exit }
    capture { print }
  ' "$path"
}

# Read an Unreleased body on stdin and decide the bump type.
# Echoes one of: minor | patch | none
# Bump rule:
#   - Any bullet under ### Added / ### Changed / ### Removed / ### Deprecated -> minor
#   - Otherwise, any bullet under ### Fixed / ### Security                    -> patch
#   - No bullets at all                                                        -> none
decide_bump() {
  awk '
    BEGIN { current = ""; minor = 0; patch = 0 }
    /^###[[:space:]]+/ {
      sub(/^###[[:space:]]+/, "", $0)
      sub(/[[:space:]]+$/, "", $0)
      current = $0
      next
    }
    /^[[:space:]]*[-*][[:space:]]+/ {
      if (current == "Added" || current == "Changed" || current == "Removed" || current == "Deprecated") {
        minor = 1
      } else if (current == "Fixed" || current == "Security") {
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

# Insert a new "## [X.Y.Z] - YYYY-MM-DD" section after the Unreleased header.
# The Unreleased body is preserved as the new version's content; Unreleased becomes empty.
# Usage: insert_release_section <changelogPath> <newVersionWithoutV> <date>
insert_release_section() {
  local path="$1"
  local version="$2"
  local date="$3"
  local tmp
  tmp="$(mktemp)"
  awk -v version="$version" -v date="$date" '
    /^## \[Unreleased\][[:space:]]*$/ && !done {
      print
      print ""
      print "## [" version "] - " date
      done = 1
      next
    }
    { print }
  ' "$path" > "$tmp"
  mv "$tmp" "$path"
}
