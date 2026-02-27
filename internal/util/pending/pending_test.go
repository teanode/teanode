package pending

import (
	"encoding/json"
	"sync"
	"testing"
)

func TestAllocateIncrementsID(t *testing.T) {
	t.Parallel()

	pending := NewRequests()
	id0, _ := pending.Allocate()
	id1, _ := pending.Allocate()
	id2, _ := pending.Allocate()

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

	pending := NewRequests()
	id, channel := pending.Allocate()

	want := Result{Data: json.RawMessage(`{"ok":true}`)}
	ok := pending.Resolve(id, want)
	if !ok {
		t.Fatal("Resolve returned false for a valid pending request")
	}

	got := <-channel
	if string(got.Data) != string(want.Data) {
		t.Fatalf("expected data %s, got %s", want.Data, got.Data)
	}
	if got.Error != "" {
		t.Fatalf("expected no error, got %q", got.Error)
	}
}

func TestResolveError(t *testing.T) {
	t.Parallel()

	pending := NewRequests()
	id, channel := pending.Allocate()

	want := Result{Error: "something went wrong"}
	pending.Resolve(id, want)

	got := <-channel
	if got.Error != want.Error {
		t.Fatalf("expected error %q, got %q", want.Error, got.Error)
	}
}

func TestResolveUnknownID(t *testing.T) {
	t.Parallel()

	pending := NewRequests()
	ok := pending.Resolve(999, Result{})
	if ok {
		t.Fatal("Resolve returned true for an unknown ID")
	}
}

func TestResolveAlreadyResolved(t *testing.T) {
	t.Parallel()

	pending := NewRequests()
	id, _ := pending.Allocate()

	pending.Resolve(id, Result{})
	ok := pending.Resolve(id, Result{})
	if ok {
		t.Fatal("Resolve returned true for an already-resolved ID")
	}
}

func TestCancel(t *testing.T) {
	t.Parallel()

	pending := NewRequests()
	id, _ := pending.Allocate()
	pending.Cancel(id)

	// After cancel, Resolve should fail.
	ok := pending.Resolve(id, Result{})
	if ok {
		t.Fatal("Resolve returned true after Cancel")
	}
}

func TestCancelUnknownID(t *testing.T) {
	t.Parallel()

	// Should not panic.
	pending := NewRequests()
	pending.Cancel(42)
}

func TestRejectAll(t *testing.T) {
	t.Parallel()

	pending := NewRequests()
	_, channel0 := pending.Allocate()
	_, channel1 := pending.Allocate()
	_, channel2 := pending.Allocate()

	pending.RejectAll("disconnected")

	for index, channel := range []<-chan Result{channel0, channel1, channel2} {
		got := <-channel
		if got.Error != "disconnected" {
			t.Fatalf("channel %d: expected error %q, got %q", index, "disconnected", got.Error)
		}
	}
}

func TestRejectAllClearsMap(t *testing.T) {
	t.Parallel()

	pending := NewRequests()
	id, _ := pending.Allocate()
	pending.RejectAll("bye")

	ok := pending.Resolve(id, Result{})
	if ok {
		t.Fatal("Resolve returned true after RejectAll")
	}
}

func TestRejectAllEmpty(t *testing.T) {
	t.Parallel()

	// Should not panic on empty map.
	pending := NewRequests()
	pending.RejectAll("no-op")
}

func TestConcurrentAllocateResolve(t *testing.T) {
	t.Parallel()

	pending := NewRequests()
	const count = 100
	var wg sync.WaitGroup
	wg.Add(count)

	type pair struct {
		id      int
		channel <-chan Result
	}
	pairs := make([]pair, count)

	// Allocate sequentially to collect pairs.
	for index := 0; index < count; index++ {
		id, channel := pending.Allocate()
		pairs[index] = pair{id, channel}
	}

	// Resolve concurrently.
	for index := 0; index < count; index++ {
		go func(result pair) {
			defer wg.Done()
			pending.Resolve(result.id, Result{Data: json.RawMessage(`"ok"`)})
		}(pairs[index])
	}

	wg.Wait()

	for index, result := range pairs {
		select {
		case received := <-result.channel:
			if string(received.Data) != `"ok"` {
				t.Fatalf("pair %d: expected \"ok\", got %s", index, received.Data)
			}
		default:
			t.Fatalf("pair %d: channel was empty after resolve", index)
		}
	}
}
