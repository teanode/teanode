package security

import (
	"io"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
)

var ulidEntropyPool = &sync.Pool{
	New: func() interface{} {
		return ulid.Monotonic(rand.New(rand.NewSource(time.Now().UnixNano())), 0)
	},
}

// NewULID returns a new lowercase ULID string.
func NewULID() string {
	entropy := ulidEntropyPool.Get()
	defer ulidEntropyPool.Put(entropy)
	return strings.ToLower(ulid.MustNew(ulid.Now(), entropy.(io.Reader)).String())
}
