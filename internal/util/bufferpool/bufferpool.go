package bufferpool

import (
	"bytes"
	"sync"
)

var bufferPool = sync.Pool{
	New: func() interface{} {
		return new(bytes.Buffer)
	},
}

func AcquireBuffer() (*bytes.Buffer, func()) {
	buffer := bufferPool.Get().(*bytes.Buffer)
	buffer.Reset()
	return buffer, func() {
		bufferPool.Put(buffer)
	}
}
