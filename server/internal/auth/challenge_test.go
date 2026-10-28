package auth

import (
	"sync"
	"testing"
	"time"
)

func newTestChallengeStore(c *clock, max int) *challengeStore {
	return &challengeStore{now: c.Now, max: max, entries: map[string]*challengeEntry{}}
}

func TestChallengeStoreInsertPrunesExpired(t *testing.T) {
	c := newClock()
	cs := newTestChallengeStore(c, 100)

	tok1, _ := cs.insert(1, true)
	tok2, _ := cs.insert(2, true)
	if len(cs.entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(cs.entries))
	}

	// Both challenges expire in 5 minutes; jump well past that and insert a
	// third one, which should sweep the two expired entries away first.
	c.Add(challengeTTL + time.Second)
	tok3, _ := cs.insert(3, true)

	if len(cs.entries) != 1 {
		t.Fatalf("entries after insert = %d, want 1 (expired ones pruned)", len(cs.entries))
	}
	if _, ok := cs.get(tok1); ok {
		t.Error("expired challenge 1 still present")
	}
	if _, ok := cs.get(tok2); ok {
		t.Error("expired challenge 2 still present")
	}
	if _, ok := cs.get(tok3); !ok {
		t.Error("freshly inserted challenge 3 is missing")
	}
}

func TestChallengeStoreEvictsTheOldestWhenFull(t *testing.T) {
	c := newClock()
	cs := newTestChallengeStore(c, 2)

	tokA, _ := cs.insert(1, true) // expires soonest: t0+5m
	c.Add(time.Minute)
	tokB, _ := cs.insert(2, true) // expires t0+6m
	if len(cs.entries) != 2 {
		t.Fatalf("entries = %d, want 2 (at capacity)", len(cs.entries))
	}

	c.Add(time.Minute)
	tokC, _ := cs.insert(3, true) // store is full and none have expired yet: evict A

	if len(cs.entries) != 2 {
		t.Fatalf("entries after eviction = %d, want 2", len(cs.entries))
	}
	if _, ok := cs.get(tokA); ok {
		t.Error("oldest (soonest-to-expire) challenge A was not evicted")
	}
	if _, ok := cs.get(tokB); !ok {
		t.Error("challenge B was evicted, want it kept")
	}
	if _, ok := cs.get(tokC); !ok {
		t.Error("newly inserted challenge C is missing")
	}
}

func TestChallengeStoreFailAndDelete(t *testing.T) {
	c := newClock()
	cs := newTestChallengeStore(c, 100)
	tok, _ := cs.insert(42, true)

	if n := cs.fail(tok); n != 1 {
		t.Errorf("fail() = %d, want 1", n)
	}
	if n := cs.fail(tok); n != 2 {
		t.Errorf("fail() = %d, want 2", n)
	}
	e, ok := cs.get(tok)
	if !ok || e.attempts != 2 {
		t.Fatalf("get() = %+v, %v, want attempts=2", e, ok)
	}

	cs.delete(tok)
	if _, ok := cs.get(tok); ok {
		t.Error("challenge still present after delete")
	}
	// fail on a deleted (unknown) token is a safe no-op.
	if n := cs.fail(tok); n != 0 {
		t.Errorf("fail() on deleted token = %d, want 0", n)
	}
}

func TestChallengeStoreGetDropsExpiredEntry(t *testing.T) {
	c := newClock()
	cs := newTestChallengeStore(c, 100)
	tok, _ := cs.insert(1, true)
	c.Add(challengeTTL + time.Second)

	if _, ok := cs.get(tok); ok {
		t.Fatal("get() returned an expired challenge")
	}
	if len(cs.entries) != 0 {
		t.Errorf("entries after get() on an expired token = %d, want 0 (dropped as a side effect)", len(cs.entries))
	}
}

// TestChallengeStoreConcurrentAccess exercises insert/get/fail/delete from
// many goroutines at once; run with -race. It only asserts the store stays
// internally consistent (no panics, no data races, no entry count blow-up),
// not any specific interleaving.
func TestChallengeStoreConcurrentAccess(t *testing.T) {
	c := newClock()
	cs := newTestChallengeStore(c, 50)

	const workers = 20
	const rounds = 100
	var wg sync.WaitGroup
	for w := range workers {
		wg.Go(func() {
			for i := range rounds {
				tok, _ := cs.insert(int64(w*rounds+i), i%2 == 0)
				cs.fail(tok)
				cs.get(tok)
				cs.delete(tok)
			}
		})
	}
	wg.Wait()

	if len(cs.entries) > cs.max {
		t.Errorf("entries = %d, want at most max = %d", len(cs.entries), cs.max)
	}
}
