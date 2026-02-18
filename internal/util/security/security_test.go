package security

import (
	"strings"
	"testing"
)

func TestGenerateRandomReturnsCorrectLength(t *testing.T) {
	t.Parallel()

	for _, length := range []int{0, 1, 16, 32, 256} {
		data := GenerateRandom(length)
		if len(data) != length {
			t.Fatalf("expected length %d, got %d", length, len(data))
		}
	}
}

func TestGenerateRandomProducesUniqueOutput(t *testing.T) {
	t.Parallel()

	first := GenerateRandom(32)
	second := GenerateRandom(32)

	firstStr := string(first)
	secondStr := string(second)
	if firstStr == secondStr {
		t.Fatal("expected two calls to GenerateRandom to produce different output")
	}
}

func TestGenerateRandomStringReturnsCorrectLength(t *testing.T) {
	t.Parallel()

	for _, length := range []int{0, 1, 10, 50, 100} {
		result := GenerateRandomString(length, LowerAlphaNumeric)
		if len(result) != length {
			t.Fatalf("expected length %d, got %d", length, len(result))
		}
	}
}

func TestGenerateRandomStringUsesOnlyAlphabet(t *testing.T) {
	t.Parallel()

	alphabets := []string{
		LowerAlpha,
		Digits,
		LowerAlphaNumeric,
		"abc",
	}

	for _, alphabet := range alphabets {
		result := GenerateRandomString(200, alphabet)
		for index, character := range result {
			if !strings.ContainsRune(alphabet, character) {
				t.Fatalf("character %q at index %d not in alphabet %q", character, index, alphabet)
			}
		}
	}
}

func TestGenerateRandomStringDigitsOnly(t *testing.T) {
	t.Parallel()

	result := GenerateRandomString(100, Digits)
	for index, character := range result {
		if character < '0' || character > '9' {
			t.Fatalf("expected digit at index %d, got %q", index, character)
		}
	}
}

func TestGenerateRandomStringProducesUniqueOutput(t *testing.T) {
	t.Parallel()

	first := GenerateRandomString(32, LowerAlphaNumeric)
	second := GenerateRandomString(32, LowerAlphaNumeric)

	if first == second {
		t.Fatal("expected two calls to GenerateRandomString to produce different output")
	}
}

func TestGenerateRandomStringEmptyLength(t *testing.T) {
	t.Parallel()

	result := GenerateRandomString(0, LowerAlphaNumeric)
	if result != "" {
		t.Fatalf("expected empty string for length 0, got %q", result)
	}
}

func TestHashPasswordReturnsNonEmptyHash(t *testing.T) {
	t.Parallel()

	hash, err := HashPassword("testpassword")
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if len(hash) == 0 {
		t.Fatal("expected non-empty hash")
	}
}

func TestHashPasswordProducesBcryptFormat(t *testing.T) {
	t.Parallel()

	hash, err := HashPassword("testpassword")
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	hashStr := string(hash)
	if !strings.HasPrefix(hashStr, "$2a$") && !strings.HasPrefix(hashStr, "$2b$") {
		t.Fatalf("expected bcrypt hash prefix, got %q", hashStr)
	}
}

func TestHashPasswordProducesDifferentHashesForSameInput(t *testing.T) {
	t.Parallel()

	first, err := HashPassword("samepassword")
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	second, err := HashPassword("samepassword")
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	if string(first) == string(second) {
		t.Fatal("expected different hashes due to random salt")
	}
}

func TestVerifyPasswordCorrectPassword(t *testing.T) {
	t.Parallel()

	password := "correcthorsebatterystaple"
	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("unexpected error hashing: %s", err)
	}

	match, err := VerifyPassword(hash, password)
	if err != nil {
		t.Fatalf("unexpected error verifying: %s", err)
	}
	if !match {
		t.Fatal("expected password to match")
	}
}

func TestVerifyPasswordWrongPassword(t *testing.T) {
	t.Parallel()

	hash, err := HashPassword("realpassword")
	if err != nil {
		t.Fatalf("unexpected error hashing: %s", err)
	}

	match, err := VerifyPassword(hash, "wrongpassword")
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if match {
		t.Fatal("expected password mismatch to return false")
	}
}

func TestVerifyPasswordInvalidHash(t *testing.T) {
	t.Parallel()

	match, err := VerifyPassword([]byte("not-a-valid-hash"), "password")
	if err == nil {
		t.Fatal("expected error for invalid hash")
	}
	if match {
		t.Fatal("expected match to be false for invalid hash")
	}
}

func TestNewULIDReturnsLowercase26Chars(t *testing.T) {
	t.Parallel()

	id := NewULID()
	if len(id) != 26 {
		t.Fatalf("expected ULID length 26, got %d", len(id))
	}
	if id != strings.ToLower(id) {
		t.Fatalf("expected lowercase ULID, got %q", id)
	}
}

func TestNewULIDProducesUniqueValues(t *testing.T) {
	t.Parallel()

	seen := make(map[string]struct{})
	for index := 0; index < 1000; index++ {
		id := NewULID()
		if _, exists := seen[id]; exists {
			t.Fatalf("duplicate ULID at iteration %d: %q", index, id)
		}
		seen[id] = struct{}{}
	}
}
