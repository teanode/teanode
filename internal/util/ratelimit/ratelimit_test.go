package ratelimit_test

import (
	"testing"
	"time"

	"github.com/teanode/teanode/internal/util/ratelimit"
)

func TestFindQuantumAndInterval(t *testing.T) {
	t.Parallel()
	for rate := ratelimit.KiloBytes; rate < 100*ratelimit.MegaBytes; rate += ratelimit.KiloBytes {
		ratelimit.NewBucketWithRate(float64(rate), ratelimit.GigaBytes)
	}
}

func TestNewBucketWithQuantumAndInterval(t *testing.T) {
	t.Parallel()
	bucket := ratelimit.NewBucketWithQuantumAndInterval(1, time.Second, 10)
	if bucket.Capacity() != 10 {
		t.Fatalf("expected capacity 10, got %d", bucket.Capacity())
	}
	if bucket.Available() != 10 {
		t.Fatalf("expected available 10, got %d", bucket.Available())
	}
}

func TestNewBucketWithRate(t *testing.T) {
	t.Parallel()
	bucket := ratelimit.NewBucketWithRate(10.0, 100)
	if bucket.Capacity() != 100 {
		t.Fatalf("expected capacity 100, got %d", bucket.Capacity())
	}
	rate := bucket.Rate()
	if rate < 9.9 || rate > 10.1 {
		t.Fatalf("expected rate ~10, got %f", rate)
	}
}

func TestTakeWithinCapacity(t *testing.T) {
	t.Parallel()
	bucket := ratelimit.NewBucketWithQuantumAndInterval(1, time.Second, 5)

	// Taking within capacity should return zero wait time.
	waitDuration := bucket.Take(3)
	if waitDuration != 0 {
		t.Fatalf("expected zero wait, got %v", waitDuration)
	}
	if bucket.Available() != 2 {
		t.Fatalf("expected 2 available, got %d", bucket.Available())
	}
}

func TestTakeExceedingCapacity(t *testing.T) {
	t.Parallel()
	bucket := ratelimit.NewBucketWithQuantumAndInterval(1, time.Second, 5)

	// Exhaust all tokens.
	bucket.Take(5)

	// Taking more should return a positive wait duration.
	waitDuration := bucket.Take(1)
	if waitDuration <= 0 {
		t.Fatalf("expected positive wait duration, got %v", waitDuration)
	}
}

func TestTakeZeroCount(t *testing.T) {
	t.Parallel()
	bucket := ratelimit.NewBucketWithQuantumAndInterval(1, time.Second, 5)

	waitDuration := bucket.Take(0)
	if waitDuration != 0 {
		t.Fatalf("expected zero wait for zero count, got %v", waitDuration)
	}
	if bucket.Available() != 5 {
		t.Fatalf("expected 5 available after zero take, got %d", bucket.Available())
	}
}

func TestTakeNegativeCount(t *testing.T) {
	t.Parallel()
	bucket := ratelimit.NewBucketWithQuantumAndInterval(1, time.Second, 5)

	waitDuration := bucket.Take(-1)
	if waitDuration != 0 {
		t.Fatalf("expected zero wait for negative count, got %v", waitDuration)
	}
	if bucket.Available() != 5 {
		t.Fatalf("expected 5 available after negative take, got %d", bucket.Available())
	}
}

func TestTakeAvailableWithinCapacity(t *testing.T) {
	t.Parallel()
	bucket := ratelimit.NewBucketWithQuantumAndInterval(1, time.Second, 5)

	taken := bucket.TakeAvailable(3)
	if taken != 3 {
		t.Fatalf("expected to take 3, got %d", taken)
	}
	if bucket.Available() != 2 {
		t.Fatalf("expected 2 available, got %d", bucket.Available())
	}
}

func TestTakeAvailableExceedingTokens(t *testing.T) {
	t.Parallel()
	bucket := ratelimit.NewBucketWithQuantumAndInterval(1, time.Second, 5)

	// Take 3, leaving 2.
	bucket.TakeAvailable(3)

	// Request 4 but only 2 are available.
	taken := bucket.TakeAvailable(4)
	if taken != 2 {
		t.Fatalf("expected to take 2, got %d", taken)
	}
	if bucket.Available() != 0 {
		t.Fatalf("expected 0 available, got %d", bucket.Available())
	}
}

func TestTakeAvailableWhenExhausted(t *testing.T) {
	t.Parallel()
	bucket := ratelimit.NewBucketWithQuantumAndInterval(1, time.Second, 5)

	bucket.TakeAvailable(5)

	taken := bucket.TakeAvailable(1)
	if taken != 0 {
		t.Fatalf("expected 0 taken when exhausted, got %d", taken)
	}
}

func TestTakeAvailableZeroCount(t *testing.T) {
	t.Parallel()
	bucket := ratelimit.NewBucketWithQuantumAndInterval(1, time.Second, 5)

	taken := bucket.TakeAvailable(0)
	if taken != 0 {
		t.Fatalf("expected 0 for zero count, got %d", taken)
	}
}

func TestTakeAvailableNegativeCount(t *testing.T) {
	t.Parallel()
	bucket := ratelimit.NewBucketWithQuantumAndInterval(1, time.Second, 5)

	taken := bucket.TakeAvailable(-1)
	if taken != 0 {
		t.Fatalf("expected 0 for negative count, got %d", taken)
	}
}

func TestTokenRefill(t *testing.T) {
	t.Parallel()
	bucket := ratelimit.NewBucketWithQuantumAndInterval(1, 10*time.Millisecond, 5)

	// Drain all tokens.
	bucket.TakeAvailable(5)
	if bucket.Available() != 0 {
		t.Fatalf("expected 0 available after drain, got %d", bucket.Available())
	}

	// Wait for refill.
	time.Sleep(35 * time.Millisecond)

	available := bucket.Available()
	if available < 2 || available > 4 {
		t.Fatalf("expected 2-4 tokens after ~35ms refill (quantum=1/10ms), got %d", available)
	}
}

func TestTokenRefillCapsAtCapacity(t *testing.T) {
	t.Parallel()
	bucket := ratelimit.NewBucketWithQuantumAndInterval(5, 10*time.Millisecond, 3)

	// Drain tokens.
	bucket.TakeAvailable(3)

	// Wait long enough to overfill if uncapped.
	time.Sleep(30 * time.Millisecond)

	if bucket.Available() != 3 {
		t.Fatalf("expected available capped at capacity 3, got %d", bucket.Available())
	}
}

func TestWaitDoesNotBlock_WhenTokensAvailable(t *testing.T) {
	t.Parallel()
	bucket := ratelimit.NewBucketWithQuantumAndInterval(1, time.Second, 5)

	start := time.Now()
	bucket.Wait(1)
	elapsed := time.Since(start)

	if elapsed > 50*time.Millisecond {
		t.Fatalf("Wait blocked for %v when tokens were available", elapsed)
	}
}

func TestWaitBlocks_WhenTokensExhausted(t *testing.T) {
	t.Parallel()
	bucket := ratelimit.NewBucketWithQuantumAndInterval(1, 20*time.Millisecond, 1)

	// Exhaust tokens.
	bucket.Take(1)

	start := time.Now()
	bucket.Wait(1)
	elapsed := time.Since(start)

	if elapsed < 10*time.Millisecond {
		t.Fatalf("Wait returned too quickly (%v), expected ~20ms block", elapsed)
	}
}

func TestResetRate(t *testing.T) {
	t.Parallel()
	bucket := ratelimit.NewBucketWithRate(10.0, 100)

	// Drain some tokens.
	bucket.TakeAvailable(50)

	// Reset with different rate and capacity.
	bucket.ResetRate(20.0, 200)

	if bucket.Capacity() != 200 {
		t.Fatalf("expected capacity 200 after reset, got %d", bucket.Capacity())
	}
	if bucket.Available() != 200 {
		t.Fatalf("expected available 200 after reset, got %d", bucket.Available())
	}
	rate := bucket.Rate()
	if rate < 19.8 || rate > 20.2 {
		t.Fatalf("expected rate ~20 after reset, got %f", rate)
	}
}

func TestResetQuantumAndInterval(t *testing.T) {
	t.Parallel()
	bucket := ratelimit.NewBucketWithQuantumAndInterval(1, time.Second, 5)

	bucket.TakeAvailable(5)
	if bucket.Available() != 0 {
		t.Fatalf("expected 0 after drain, got %d", bucket.Available())
	}

	bucket.ResetQuantumAndInterval(2, 100*time.Millisecond, 10)

	if bucket.Capacity() != 10 {
		t.Fatalf("expected capacity 10 after reset, got %d", bucket.Capacity())
	}
	if bucket.Available() != 10 {
		t.Fatalf("expected available 10 after reset, got %d", bucket.Available())
	}
}

func TestMultipleTakesTrackCorrectly(t *testing.T) {
	t.Parallel()
	bucket := ratelimit.NewBucketWithQuantumAndInterval(1, time.Second, 10)

	bucket.Take(3)
	bucket.Take(2)
	taken := bucket.TakeAvailable(10)

	if taken != 5 {
		t.Fatalf("expected 5 remaining after taking 3+2 from 10, got %d", taken)
	}
}

func TestTakeWaitDurationScalesWithDeficit(t *testing.T) {
	t.Parallel()
	bucket := ratelimit.NewBucketWithQuantumAndInterval(1, time.Second, 5)

	// Exhaust tokens.
	bucket.Take(5)

	waitForOne := bucket.Take(1)
	waitForTwo := bucket.Take(2)

	// More deficit should mean longer wait.
	if waitForTwo <= waitForOne {
		t.Fatalf("expected larger wait for bigger deficit: 1-token wait=%v, 2-token wait=%v", waitForOne, waitForTwo)
	}
}
