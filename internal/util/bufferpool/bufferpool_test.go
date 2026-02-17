package bufferpool

import (
	"sync"
	"testing"
)

func TestAcquireBufferReturnsEmptyBuffer(t *testing.T) {
	t.Parallel()

	buf, release := AcquireBuffer()
	defer release()

	if buf.Len() != 0 {
		t.Fatalf("expected empty buffer, got length %d", buf.Len())
	}
}

func TestAcquireBufferIsWritable(t *testing.T) {
	t.Parallel()

	buf, release := AcquireBuffer()
	defer release()

	data := []byte("hello world")
	n, err := buf.Write(data)
	if err != nil {
		t.Fatalf("unexpected write error: %s", err)
	}
	if n != len(data) {
		t.Fatalf("expected to write %d bytes, wrote %d", len(data), n)
	}
	if buf.String() != "hello world" {
		t.Fatalf("expected %q, got %q", "hello world", buf.String())
	}
}

func TestAcquireBufferResetsOnReuse(t *testing.T) {
	t.Parallel()

	// Write data, release, then re-acquire and check it's empty.
	buf, release := AcquireBuffer()
	buf.WriteString("leftover data")
	release()

	buf2, release2 := AcquireBuffer()
	defer release2()

	if buf2.Len() != 0 {
		t.Fatalf("expected reused buffer to be empty, got length %d", buf2.Len())
	}
}

func TestAcquireBufferConcurrent(t *testing.T) {
	t.Parallel()

	const n = 100
	var wg sync.WaitGroup
	wg.Add(n)

	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			buf, release := AcquireBuffer()
			defer release()

			buf.WriteString("test")
			if buf.String() != "test" {
				t.Errorf("expected %q, got %q", "test", buf.String())
			}
		}()
	}

	wg.Wait()
}
