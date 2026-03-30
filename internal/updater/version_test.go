package updater

import "testing"

func TestParseSemver(t *testing.T) {
	tests := []struct {
		input        string
		major        int
		minor        int
		patch        int
		prerelease   string
		commitsAhead int
		wantError    bool
	}{
		{"1.2.3", 1, 2, 3, "", 0, false},
		{"v0.1.4", 0, 1, 4, "", 0, false},
		{"10.20.30", 10, 20, 30, "", 0, false},
		{"1.0.0-beta.1", 1, 0, 0, "beta.1", 0, false},
		{"1.0.0+build123", 1, 0, 0, "", 0, false},
		{"1.0.0-rc.1+build", 1, 0, 0, "rc.1", 0, false},

		// Git-describe versions.
		{"v0.1.4-2-g2191b71", 0, 1, 4, "", 2, false},
		{"0.1.4-10-gabcdef0", 0, 1, 4, "", 10, false},
		{"1.0.0-1-g0000000", 1, 0, 0, "", 1, false},

		// Errors.
		{"invalid", 0, 0, 0, "", 0, true},
		{"1.2", 0, 0, 0, "", 0, true},
		{"a.b.c", 0, 0, 0, "", 0, true},
	}

	for _, testCase := range tests {
		result, err := parseSemver(testCase.input)
		if testCase.wantError {
			if err == nil {
				t.Errorf("parseSemver(%q) expected error, got nil", testCase.input)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseSemver(%q) unexpected error: %v", testCase.input, err)
			continue
		}
		if result.Major != testCase.major || result.Minor != testCase.minor || result.Patch != testCase.patch {
			t.Errorf("parseSemver(%q) = %d.%d.%d, want %d.%d.%d",
				testCase.input, result.Major, result.Minor, result.Patch,
				testCase.major, testCase.minor, testCase.patch)
		}
		if result.Prerelease != testCase.prerelease {
			t.Errorf("parseSemver(%q).Prerelease = %q, want %q",
				testCase.input, result.Prerelease, testCase.prerelease)
		}
		if result.CommitsAhead != testCase.commitsAhead {
			t.Errorf("parseSemver(%q).CommitsAhead = %d, want %d",
				testCase.input, result.CommitsAhead, testCase.commitsAhead)
		}
	}
}

func TestIsNewer(t *testing.T) {
	tests := []struct {
		remote    string
		local     string
		expected  bool
		wantError bool
	}{
		// Basic comparisons.
		{"0.2.0", "0.1.0", true, false},
		{"0.1.0", "0.2.0", false, false},
		{"0.1.0", "0.1.0", false, false},
		{"1.0.0", "0.9.9", true, false},
		{"0.1.5", "0.1.4", true, false},
		{"0.1.4", "0.1.5", false, false},

		// With v prefix.
		{"v0.2.0", "v0.1.0", true, false},
		{"v0.1.0", "0.1.0", false, false},

		// Prerelease handling.
		{"1.0.0", "1.0.0-beta.1", true, false},  // stable > prerelease
		{"1.0.0-beta.1", "1.0.0", false, false}, // prerelease < stable
		{"1.0.0-beta.2", "1.0.0-beta.1", true, false},
		{"1.0.0-alpha", "1.0.0-beta", false, false}, // alpha < beta lexicographically

		// Git-describe: local is ahead of release — no update offered.
		{"0.1.4", "v0.1.4-2-g2191b71", false, false},
		{"0.1.4", "0.1.4-10-gabcdef0", false, false},
		// Git-describe: remote is a newer release than the tag local is based on.
		{"0.2.0", "v0.1.4-2-g2191b71", true, false},
		// Git-describe: remote is an older release than the tag local is based on.
		{"0.1.3", "v0.1.4-2-g2191b71", false, false},

		// Invalid versions.
		{"invalid", "0.1.0", false, true},
		{"0.1.0", "invalid", false, true},
	}

	for _, testCase := range tests {
		result, err := IsNewer(testCase.remote, testCase.local)
		if testCase.wantError {
			if err == nil {
				t.Errorf("IsNewer(%q, %q) expected error, got nil", testCase.remote, testCase.local)
			}
			continue
		}
		if err != nil {
			t.Errorf("IsNewer(%q, %q) unexpected error: %v", testCase.remote, testCase.local, err)
			continue
		}
		if result != testCase.expected {
			t.Errorf("IsNewer(%q, %q) = %v, want %v",
				testCase.remote, testCase.local, result, testCase.expected)
		}
	}
}

func TestIsAheadOfRelease(t *testing.T) {
	tests := []struct {
		remote   string
		local    string
		expected bool
	}{
		// Local is ahead of the same release.
		{"0.1.4", "v0.1.4-2-g2191b71", true},
		{"0.1.4", "0.1.4-10-gabcdef0", true},
		// Local matches release exactly — not ahead.
		{"0.1.4", "0.1.4", false},
		// Local is ahead but of a different release version.
		{"0.2.0", "v0.1.4-2-g2191b71", false},
		// Invalid versions return false.
		{"invalid", "v0.1.4-2-g2191b71", false},
		{"0.1.4", "invalid", false},
	}

	for _, testCase := range tests {
		result := IsAheadOfRelease(testCase.remote, testCase.local)
		if result != testCase.expected {
			t.Errorf("IsAheadOfRelease(%q, %q) = %v, want %v",
				testCase.remote, testCase.local, result, testCase.expected)
		}
	}
}
