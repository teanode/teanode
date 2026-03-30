package updater

import "testing"

func TestParseSemver(t *testing.T) {
	tests := []struct {
		input      string
		major      int
		minor      int
		patch      int
		prerelease string
		wantError  bool
	}{
		{"1.2.3", 1, 2, 3, "", false},
		{"v0.1.4", 0, 1, 4, "", false},
		{"10.20.30", 10, 20, 30, "", false},
		{"1.0.0-beta.1", 1, 0, 0, "beta.1", false},
		{"1.0.0+build123", 1, 0, 0, "", false},
		{"1.0.0-rc.1+build", 1, 0, 0, "rc.1", false},
		{"invalid", 0, 0, 0, "", true},
		{"1.2", 0, 0, 0, "", true},
		{"a.b.c", 0, 0, 0, "", true},
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
