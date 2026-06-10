package security

import (
	"crypto/rand"
	"io"
	"strings"
	"sync"

	"github.com/oklog/ulid/v2"
)

// ulidEntropyPool provides monotonic ULID entropy backed by crypto/rand.
// ULIDs are used for session and token identifiers, so the entropy source
// must be cryptographically secure — a seeded math/rand source would make
// identifiers predictable.
var ulidEntropyPool = &sync.Pool{
	New: func() interface{} {
		return ulid.Monotonic(rand.Reader, 0)
	},
}

// NewULID returns a new lowercase ULID string.
func NewULID() string {
	entropy := ulidEntropyPool.Get()
	defer ulidEntropyPool.Put(entropy)
	return strings.ToLower(ulid.MustNew(ulid.Now(), entropy.(io.Reader)).String())
}
