package bufferpool

import (
	"sync"
	"testing"
)

func TestAcquireBufferReturnsEmptyBuffer(t *testing.T) {
	t.Parallel()

	buffer, release := AcquireBuffer()
	defer release()

	if buffer.Len() != 0 {
		t.Fatalf("expected empty buffer, got length %d", buffer.Len())
	}
}

func TestAcquireBufferIsWritable(t *testing.T) {
	t.Parallel()

	buffer, release := AcquireBuffer()
	defer release()

	data := []byte("hello world")
	count, err := buffer.Write(data)
	if err != nil {
		t.Fatalf("unexpected write error: %s", err)
	}
	if count != len(data) {
		t.Fatalf("expected to write %d bytes, wrote %d", len(data), count)
	}
	if buffer.String() != "hello world" {
		t.Fatalf("expected %q, got %q", "hello world", buffer.String())
	}
}

func TestAcquireBufferResetsOnReuse(t *testing.T) {
	t.Parallel()

	// Write data, release, then re-acquire and check it's empty.
	buffer, release := AcquireBuffer()
	buffer.WriteString("leftover data")
	release()

	buf2, release2 := AcquireBuffer()
	defer release2()

	if buf2.Len() != 0 {
		t.Fatalf("expected reused buffer to be empty, got length %d", buf2.Len())
	}
}

func TestAcquireBufferConcurrent(t *testing.T) {
	t.Parallel()

	const count = 100
	var wg sync.WaitGroup
	wg.Add(count)

	for index := 0; index < count; index++ {
		go func() {
			defer wg.Done()
			buffer, release := AcquireBuffer()
			defer release()

			buffer.WriteString("test")
			if buffer.String() != "test" {
				t.Errorf("expected %q, got %q", "test", buffer.String())
			}
		}()
	}

	wg.Wait()
}
