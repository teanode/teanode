package ulid

import (
	"sync"
	"testing"
)

func TestGenerateStringLength(t *testing.T) {
	t.Parallel()

	id := GenerateString()
	// ULID is 26 characters in Crockford's base32.
	if len(id) != 26 {
		t.Fatalf("expected length 26, got %d (%q)", len(id), id)
	}
}

func TestGenerateStringLowercase(t *testing.T) {
	t.Parallel()

	id := GenerateString()
	for _, c := range id {
		if c >= 'A' && c <= 'Z' {
			t.Fatalf("expected lowercase, found uppercase character in %q", id)
		}
	}
}

func TestGenerateStringUnique(t *testing.T) {
	t.Parallel()

	const n = 1000
	seen := make(map[string]struct{}, n)
	for i := 0; i < n; i++ {
		id := GenerateString()
		if _, ok := seen[id]; ok {
			t.Fatalf("duplicate ULID after %d generations: %q", i, id)
		}
		seen[id] = struct{}{}
	}
}

func TestGenerateStringValidCharacters(t *testing.T) {
	t.Parallel()

	// Crockford's base32 lowercase: 0-9, a-z excluding i, l, o, u.
	valid := "0123456789abcdefghjkmnpqrstvwxyz"
	validSet := make(map[rune]bool, len(valid))
	for _, c := range valid {
		validSet[c] = true
	}

	for i := 0; i < 100; i++ {
		id := GenerateString()
		for _, c := range id {
			if !validSet[c] {
				t.Fatalf("invalid character %q in ULID %q", c, id)
			}
		}
	}
}

func TestGenerateStringConcurrent(t *testing.T) {
	t.Parallel()

	const n = 100
	var wg sync.WaitGroup
	wg.Add(n)

	ids := make([]string, n)
	for i := 0; i < n; i++ {
		go func(idx int) {
			defer wg.Done()
			ids[idx] = GenerateString()
		}(i)
	}

	wg.Wait()

	seen := make(map[string]struct{}, n)
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			t.Fatalf("duplicate ULID in concurrent generation: %q", id)
		}
		seen[id] = struct{}{}
	}
}
