package updater

import "testing"

func TestNormalizePolicy(t *testing.T) {
	tests := []struct {
		name     string
		input    Policy
		expected Policy
		valid    bool
	}{
		{name: "disabled", input: PolicyDisabled, expected: PolicyDisabled, valid: true},
		{name: "notify", input: PolicyNotify, expected: PolicyNotify, valid: true},
		{name: "auto", input: PolicyAuto, expected: PolicyAuto, valid: true},
		{name: "empty", input: "", expected: PolicyNotify, valid: false},
		{name: "invalid", input: Policy("surprise"), expected: PolicyNotify, valid: false},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			if got := NormalizePolicy(testCase.input); got != testCase.expected {
				t.Fatalf("NormalizePolicy(%q) = %q, want %q", testCase.input, got, testCase.expected)
			}
			if got := IsValidPolicy(testCase.input); got != testCase.valid {
				t.Fatalf("IsValidPolicy(%q) = %v, want %v", testCase.input, got, testCase.valid)
			}
		})
	}
}
