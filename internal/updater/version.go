package updater

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// gitDescribePattern matches the suffix added by `git describe` when a build
// has commits beyond a tag: "<commits>-g<hex>", e.g. "2-g2191b71".
var gitDescribePattern = regexp.MustCompile(`^(\d+)-g([0-9a-f]+)$`)

// semver represents a parsed semantic version.
type semver struct {
	Major      int
	Minor      int
	Patch      int
	Prerelease string
	// CommitsAhead is non-zero when the version was produced by git-describe
	// and the build is N commits ahead of the tag (e.g. v0.1.4-2-g2191b71).
	CommitsAhead int
}

// parseSemver parses a version string like "1.2.3", "1.2.3-beta.1",
// or a git-describe string like "v0.1.4-2-g2191b71".
func parseSemver(version string) (semver, error) {
	version = strings.TrimPrefix(version, "v")

	// Split off build metadata (ignored for comparison).
	if index := strings.IndexByte(version, '+'); index >= 0 {
		version = version[:index]
	}

	// Split off prerelease / git-describe suffix.
	prerelease := ""
	commitsAhead := 0
	if index := strings.IndexByte(version, '-'); index >= 0 {
		suffix := version[index+1:]
		version = version[:index]

		if match := gitDescribePattern.FindStringSubmatch(suffix); match != nil {
			ahead, _ := strconv.Atoi(match[1])
			commitsAhead = ahead
		} else {
			prerelease = suffix
		}
	}

	parts := strings.SplitN(version, ".", 3)
	if len(parts) != 3 {
		return semver{}, fmt.Errorf("updater: invalid semver: %q", version)
	}

	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return semver{}, fmt.Errorf("updater: invalid major version: %w", err)
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return semver{}, fmt.Errorf("updater: invalid minor version: %w", err)
	}
	patch, err := strconv.Atoi(parts[2])
	if err != nil {
		return semver{}, fmt.Errorf("updater: invalid patch version: %w", err)
	}

	return semver{
		Major:        major,
		Minor:        minor,
		Patch:        patch,
		Prerelease:   prerelease,
		CommitsAhead: commitsAhead,
	}, nil
}

// IsNewer reports whether the remote version is newer than the local version.
func IsNewer(remoteVersion, localVersion string) (bool, error) {
	remote, err := parseSemver(remoteVersion)
	if err != nil {
		return false, fmt.Errorf("updater: parsing remote version: %w", err)
	}
	local, err := parseSemver(localVersion)
	if err != nil {
		return false, fmt.Errorf("updater: parsing local version: %w", err)
	}

	if remote.Major != local.Major {
		return remote.Major > local.Major, nil
	}
	if remote.Minor != local.Minor {
		return remote.Minor > local.Minor, nil
	}
	if remote.Patch != local.Patch {
		return remote.Patch > local.Patch, nil
	}

	// A local build with commits ahead of a tag (git-describe) is newer than
	// the corresponding release (e.g. v0.1.4-2-g2191b71 > 0.1.4).
	if local.CommitsAhead > 0 && remote.CommitsAhead == 0 {
		return false, nil
	}
	if remote.CommitsAhead > 0 && local.CommitsAhead == 0 {
		return true, nil
	}
	if local.CommitsAhead != remote.CommitsAhead {
		return remote.CommitsAhead > local.CommitsAhead, nil
	}

	// If local has no prerelease but remote does, local is newer (stable > prerelease).
	// If both have prerelease, compare lexicographically.
	if local.Prerelease == "" && remote.Prerelease != "" {
		return false, nil
	}
	if local.Prerelease != "" && remote.Prerelease == "" {
		return true, nil
	}
	return remote.Prerelease > local.Prerelease, nil
}

// IsAheadOfRelease reports whether the local version has commits beyond the
// latest release tag (i.e. it is a git-describe development build).
func IsAheadOfRelease(remoteVersion, localVersion string) bool {
	remote, err := parseSemver(remoteVersion)
	if err != nil {
		return false
	}
	local, err := parseSemver(localVersion)
	if err != nil {
		return false
	}
	return local.CommitsAhead > 0 &&
		remote.Major == local.Major &&
		remote.Minor == local.Minor &&
		remote.Patch == local.Patch
}
