package auth

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestLimiterBlocksAfterMaxFailures(t *testing.T) {
	now := time.Unix(1000, 0)
	l := newLimiter(3, time.Minute, func() time.Time { return now })

	for i := range 3 {
		if !l.Allow("k") {
			t.Fatalf("blocked before reaching the limit (after %d failures)", i)
		}
		l.Fail("k")
	}
	if l.Allow("k") {
		t.Fatal("not blocked after 3 failures")
	}
	if !l.Allow("other") {
		t.Error("an unrelated key was blocked")
	}
	now = now.Add(time.Minute + time.Second)
	if !l.Allow("k") {
		t.Error("still blocked after the window passed")
	}
}

func TestLimiterResetClearsFailures(t *testing.T) {
	now := time.Unix(1000, 0)
	l := newLimiter(2, time.Minute, func() time.Time { return now })
	l.Fail("k")
	l.Reset("k")
	l.Fail("k")
	if !l.Allow("k") {
		t.Error("failures before Reset were still counted")
	}
}

func TestLimiterRemainingCountsDownToZero(t *testing.T) {
	now := time.Unix(1000, 0)
	l := newLimiter(5, 15*time.Minute, func() time.Time { return now })
	if r := l.Remaining("k"); r != 5 {
		t.Errorf("initial Remaining = %d, want 5", r)
	}
	for want := 4; want >= 0; want-- {
		l.Fail("k")
		if r := l.Remaining("k"); r != want {
			t.Errorf("Remaining after failure = %d, want %d", r, want)
		}
	}
}

func TestLimiterRetryAfterCountsDownFromWindow(t *testing.T) {
	now := time.Unix(1000, 0)
	l := newLimiter(5, 15*time.Minute, func() time.Time { return now })
	if ra := l.RetryAfter("k"); ra != 0 {
		t.Errorf("RetryAfter before any failure = %v, want 0", ra)
	}
	for range 5 {
		l.Fail("k")
	}
	if ra := l.RetryAfter("k"); ra != 15*time.Minute {
		t.Errorf("RetryAfter right after locking = %v, want 15m", ra)
	}
	now = now.Add(5 * time.Minute)
	if ra := l.RetryAfter("k"); ra != 10*time.Minute {
		t.Errorf("RetryAfter after 5m elapsed = %v, want 10m", ra)
	}
}

func TestLimiterWindowExpiryResetsCount(t *testing.T) {
	now := time.Unix(1000, 0)
	l := newLimiter(5, 15*time.Minute, func() time.Time { return now })
	for range 5 {
		l.Fail("k")
	}
	now = now.Add(15*time.Minute + time.Second)
	if r := l.Remaining("k"); r != 5 {
		t.Errorf("Remaining after the window expired = %d, want 5", r)
	}
	if ra := l.RetryAfter("k"); ra != 0 {
		t.Errorf("RetryAfter after the window expired = %v, want 0", ra)
	}
	if !l.Allow("k") {
		t.Error("still blocked after the window expired")
	}
}

// TestLimiterReserveCountsAtomically covers Reserve: while the key is not
// blocked it records one failure and returns true; once max failures are
// recorded it returns false without counting further.
func TestLimiterReserveCountsAtomically(t *testing.T) {
	now := time.Unix(1000, 0)
	l := newLimiter(3, time.Minute, func() time.Time { return now })

	for i := range 3 {
		if _, ok := l.Reserve("k"); !ok {
			t.Fatalf("Reserve refused before reaching the limit (after %d)", i)
		}
		if got, want := l.Remaining("k"), 3-(i+1); got != want {
			t.Errorf("Remaining after %d reservations = %d, want %d", i+1, got, want)
		}
	}
	if _, ok := l.Reserve("k"); ok {
		t.Fatal("Reserve allowed after 3 recorded failures")
	}
	if l.Remaining("k") != 0 {
		t.Errorf("Remaining = %d after a refused Reserve, want 0", l.Remaining("k"))
	}
	now = now.Add(time.Minute + time.Second)
	if _, ok := l.Reserve("k"); !ok {
		t.Error("Reserve still refused after the window passed")
	}
}

// TestLimiterReserveUnderConcurrency: however many goroutines race on one
// key, exactly max reservations succeed.
func TestLimiterReserveUnderConcurrency(t *testing.T) {
	now := time.Unix(1000, 0)
	l := newLimiter(5, time.Minute, func() time.Time { return now })
	var wg sync.WaitGroup
	var granted atomic.Int32
	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, ok := l.Reserve("k"); ok {
				granted.Add(1)
			}
		}()
	}
	wg.Wait()
	if got := granted.Load(); got != 5 {
		t.Errorf("granted reservations = %d, want exactly 5", got)
	}
}

// TestLimiterReleaseUndoesOneReservation covers Release: it takes back one
// recorded failure (a reservation that turned out not to be a failure) and
// never goes below zero or touches an absent key.
func TestLimiterReleaseUndoesOneReservation(t *testing.T) {
	now := time.Unix(1000, 0)
	l := newLimiter(3, time.Minute, func() time.Time { return now })

	l.Fail("k")
	gen, _ := l.Reserve("k")
	l.Release("k", gen)
	if got := l.Remaining("k"); got != 2 {
		t.Errorf("Remaining after Fail+Reserve+Release = %d, want 2", got)
	}
	l.Release("k", gen)
	l.Release("k", gen)
	if got := l.Remaining("k"); got != 3 {
		t.Errorf("Remaining after extra Releases = %d, want 3", got)
	}
	l.Release("absent", gen)
	if got := l.Remaining("absent"); got != 3 {
		t.Errorf("Remaining(absent) = %d, want 3", got)
	}
}

// TestLimiterSettleRecountsAReservationWipedByReset: a reservation cleared
// by a concurrent Reset (another request's successful login) must still end
// up counted when its own request fails.
func TestLimiterSettleRecountsAReservationWipedByReset(t *testing.T) {
	now := time.Unix(1000, 0)
	l := newLimiter(5, time.Minute, func() time.Time { return now })

	gen, _ := l.Reserve("k")
	l.Reset("k")
	l.Settle("k", gen)
	if got := l.Remaining("k"); got != 4 {
		t.Errorf("Remaining after Reserve+Reset+Settle = %d, want 4 (one failure)", got)
	}
}

// TestLimiterSettleWithoutResetDoesNotDoubleCount: when nothing reset the
// key, the reservation already is the failure; Settle adds nothing.
func TestLimiterSettleWithoutResetDoesNotDoubleCount(t *testing.T) {
	now := time.Unix(1000, 0)
	l := newLimiter(5, time.Minute, func() time.Time { return now })

	gen, _ := l.Reserve("k")
	l.Reserve("k")
	l.Settle("k", gen)
	if got := l.Remaining("k"); got != 3 {
		t.Errorf("Remaining after 2 Reserves + Settle = %d, want 3", got)
	}
}

// TestLimiterSettleRespectsTheLimit: a recount never pushes the count past
// max.
func TestLimiterSettleRespectsTheLimit(t *testing.T) {
	now := time.Unix(1000, 0)
	l := newLimiter(2, time.Minute, func() time.Time { return now })

	g1, _ := l.Reserve("k")
	g2, _ := l.Reserve("k")
	g3, _ := l.Reserve("k") // refused
	l.Reset("k")
	l.Settle("k", g1)
	l.Settle("k", g2)
	l.Settle("k", g3)
	if got := l.Remaining("k"); got != 0 {
		t.Errorf("Remaining = %d, want 0", got)
	}
	l.mu.Lock()
	count := 0
	if f := l.fails["k"]; f != nil {
		count = f.count
	}
	l.mu.Unlock()
	if count != 2 {
		t.Errorf("count = %d after recounts, want capped at 2", count)
	}
}

// TestLimiterReleaseAfterResetIsANoOp: a reservation already cleared by a
// Reset must not take back a failure recorded after it.
func TestLimiterReleaseAfterResetIsANoOp(t *testing.T) {
	now := time.Unix(1000, 0)
	l := newLimiter(5, time.Minute, func() time.Time { return now })

	gen, _ := l.Reserve("k")
	l.Reset("k")
	l.Fail("k")
	l.Release("k", gen)
	if got := l.Remaining("k"); got != 4 {
		t.Errorf("Remaining = %d, want 4 (the later failure kept)", got)
	}
}
