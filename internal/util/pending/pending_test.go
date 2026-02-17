package pending

import (
	"encoding/json"
	"sync"
	"testing"
)

func TestAllocateIncrementsID(t *testing.T) {
	t.Parallel()

	r := NewRequests()
	id0, _ := r.Allocate()
	id1, _ := r.Allocate()
	id2, _ := r.Allocate()

	if id0 != 0 {
		t.Fatalf("expected first id to be 0, got %d", id0)
	}
	if id1 != 1 {
		t.Fatalf("expected second id to be 1, got %d", id1)
	}
	if id2 != 2 {
		t.Fatalf("expected third id to be 2, got %d", id2)
	}
}

func TestResolve(t *testing.T) {
	t.Parallel()

	r := NewRequests()
	id, ch := r.Allocate()

	want := Result{Data: json.RawMessage(`{"ok":true}`)}
	ok := r.Resolve(id, want)
	if !ok {
		t.Fatal("Resolve returned false for a valid pending request")
	}

	got := <-ch
	if string(got.Data) != string(want.Data) {
		t.Fatalf("expected data %s, got %s", want.Data, got.Data)
	}
	if got.Error != "" {
		t.Fatalf("expected no error, got %q", got.Error)
	}
}

func TestResolveError(t *testing.T) {
	t.Parallel()

	r := NewRequests()
	id, ch := r.Allocate()

	want := Result{Error: "something went wrong"}
	r.Resolve(id, want)

	got := <-ch
	if got.Error != want.Error {
		t.Fatalf("expected error %q, got %q", want.Error, got.Error)
	}
}

func TestResolveUnknownID(t *testing.T) {
	t.Parallel()

	r := NewRequests()
	ok := r.Resolve(999, Result{})
	if ok {
		t.Fatal("Resolve returned true for an unknown ID")
	}
}

func TestResolveAlreadyResolved(t *testing.T) {
	t.Parallel()

	r := NewRequests()
	id, _ := r.Allocate()

	r.Resolve(id, Result{})
	ok := r.Resolve(id, Result{})
	if ok {
		t.Fatal("Resolve returned true for an already-resolved ID")
	}
}

func TestCancel(t *testing.T) {
	t.Parallel()

	r := NewRequests()
	id, _ := r.Allocate()
	r.Cancel(id)

	// After cancel, Resolve should fail.
	ok := r.Resolve(id, Result{})
	if ok {
		t.Fatal("Resolve returned true after Cancel")
	}
}

func TestCancelUnknownID(t *testing.T) {
	t.Parallel()

	// Should not panic.
	r := NewRequests()
	r.Cancel(42)
}

func TestRejectAll(t *testing.T) {
	t.Parallel()

	r := NewRequests()
	_, ch0 := r.Allocate()
	_, ch1 := r.Allocate()
	_, ch2 := r.Allocate()

	r.RejectAll("disconnected")

	for i, ch := range []<-chan Result{ch0, ch1, ch2} {
		got := <-ch
		if got.Error != "disconnected" {
			t.Fatalf("channel %d: expected error %q, got %q", i, "disconnected", got.Error)
		}
	}
}

func TestRejectAllClearsMap(t *testing.T) {
	t.Parallel()

	r := NewRequests()
	id, _ := r.Allocate()
	r.RejectAll("bye")

	ok := r.Resolve(id, Result{})
	if ok {
		t.Fatal("Resolve returned true after RejectAll")
	}
}

func TestRejectAllEmpty(t *testing.T) {
	t.Parallel()

	// Should not panic on empty map.
	r := NewRequests()
	r.RejectAll("no-op")
}

func TestConcurrentAllocateResolve(t *testing.T) {
	t.Parallel()

	r := NewRequests()
	const n = 100
	var wg sync.WaitGroup
	wg.Add(n)

	type pair struct {
		id int
		ch <-chan Result
	}
	pairs := make([]pair, n)

	// Allocate sequentially to collect pairs.
	for i := 0; i < n; i++ {
		id, ch := r.Allocate()
		pairs[i] = pair{id, ch}
	}

	// Resolve concurrently.
	for i := 0; i < n; i++ {
		go func(p pair) {
			defer wg.Done()
			r.Resolve(p.id, Result{Data: json.RawMessage(`"ok"`)})
		}(pairs[i])
	}

	wg.Wait()

	for i, p := range pairs {
		select {
		case res := <-p.ch:
			if string(res.Data) != `"ok"` {
				t.Fatalf("pair %d: expected \"ok\", got %s", i, res.Data)
			}
		default:
			t.Fatalf("pair %d: channel was empty after resolve", i)
		}
	}
}
