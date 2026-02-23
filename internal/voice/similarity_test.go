package voice

import "testing"

func TestSimilarity_EditDistance(t *testing.T) {
	tests := []struct {
		name     string
		a        string
		b        string
		expected float64
	}{
		{name: "exact", a: "hello world", b: "hello world", expected: 1},
		{name: "single substitution", a: "hello", b: "hallo", expected: 0.8},
		{name: "empty", a: "", b: "", expected: 1},
		{name: "completely different", a: "abc", b: "xyz", expected: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := textSimilarity(test.a, test.b)
			if got != test.expected {
				t.Fatalf("unexpected similarity for %q vs %q: got %.4f want %.4f", test.a, test.b, got, test.expected)
			}
		})
	}
}
