package updater

import (
	"fmt"
	"strconv"
	"strings"
)

// semver represents a parsed semantic version.
type semver struct {
	Major      int
	Minor      int
	Patch      int
	Prerelease string
}

// parseSemver parses a version string like "1.2.3" or "1.2.3-beta.1".
func parseSemver(version string) (semver, error) {
	version = strings.TrimPrefix(version, "v")

	// Split off build metadata (ignored for comparison).
	if index := strings.IndexByte(version, '+'); index >= 0 {
		version = version[:index]
	}

	// Split off prerelease.
	prerelease := ""
	if index := strings.IndexByte(version, '-'); index >= 0 {
		prerelease = version[index+1:]
		version = version[:index]
	}

	parts := strings.SplitN(version, ".", 3)
	if len(parts) != 3 {
		return semver{}, fmt.Errorf("invalid semver: %q", version)
	}

	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return semver{}, fmt.Errorf("invalid major version: %w", err)
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return semver{}, fmt.Errorf("invalid minor version: %w", err)
	}
	patch, err := strconv.Atoi(parts[2])
	if err != nil {
		return semver{}, fmt.Errorf("invalid patch version: %w", err)
	}

	return semver{
		Major:      major,
		Minor:      minor,
		Patch:      patch,
		Prerelease: prerelease,
	}, nil
}

// IsNewer reports whether the remote version is newer than the local version.
func IsNewer(remoteVersion, localVersion string) (bool, error) {
	remote, err := parseSemver(remoteVersion)
	if err != nil {
		return false, fmt.Errorf("parsing remote version: %w", err)
	}
	local, err := parseSemver(localVersion)
	if err != nil {
		return false, fmt.Errorf("parsing local version: %w", err)
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
