package ulid

import (
	"io"
	"math/rand"
	"strings"
	"sync"
	"time"

	oklogulid "github.com/oklog/ulid/v2"
)

var entropyPool = &sync.Pool{
	New: func() interface{} {
		return oklogulid.Monotonic(rand.New(rand.NewSource(time.Now().UnixNano())), 0)
	},
}

// GenerateString returns a new lowercase ULID string.
func GenerateString() string {
	entropy := entropyPool.Get()
	defer entropyPool.Put(entropy)
	return strings.ToLower(oklogulid.MustNew(oklogulid.Now(), entropy.(io.Reader)).String())
}
