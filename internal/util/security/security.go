// Package security provides shared security and credential helpers.
package security

import (
	"crypto/rand"
	"io"
)

const (
	LowerAlpha        = "abcdefghijklmnopqrstuvwxyz"
	Digits            = "0123456789"
	LowerAlphaNumeric = LowerAlpha + Digits
)

// GenerateRandom returns a random byte slice of the given length.
func GenerateRandom(length int) []byte {
	data := make([]byte, length)
	if _, err := io.ReadFull(rand.Reader, data); err != nil {
		panic(err)
	}
	return data
}

// GenerateRandomString returns a random string of the given length drawn from
// the provided alphabet.
func GenerateRandomString(length int, alphabet string) string {
	choices := []byte(alphabet)
	size := len(choices)
	randomString := make([]byte, length)
	for index, data := range GenerateRandom(length) {
		randomString[index] = choices[int(data)%size]
	}
	return string(randomString)
}
