package updater

import "testing"

func TestContainsAny(t *testing.T) {
	tests := []struct {
		haystack string
		needles  []string
		expected bool
	}{
		{"12:docker:/some/path", []string{"docker"}, true},
		{"12:kubepods:/some/path", []string{"docker", "kubepods"}, true},
		{"12:cpuset:/", []string{"docker", "kubepods"}, false},
		{"", []string{"docker"}, false},
		{"something", []string{}, false},
		{"short", []string{"toolong"}, false},
	}

	for _, testCase := range tests {
		result := containsAny(testCase.haystack, testCase.needles...)
		if result != testCase.expected {
			t.Errorf("containsAny(%q, %v) = %v, want %v",
				testCase.haystack, testCase.needles, result, testCase.expected)
		}
	}
}
