package auth

import (
	"sync"
	"time"
)

// challengeTTL is how long a two-factor login challenge stays valid.
const challengeTTL = 5 * time.Minute

// maxChallenges caps the number of challenges kept in memory at once, so a
// flood of first-step logins cannot grow this map without bound.
const maxChallenges = 10000

// challengeEntry is what a login challenge remembers between the first step
// (password) and the second (TOTP code or recovery code). Only the SHA-256
// of the raw challenge value is ever stored; see challengeStore.insert.
type challengeEntry struct {
	userID    int64
	remember  bool
	expiresAt time.Time
	attempts  int
}

// challengeStore holds in-flight two-factor login challenges in memory. A
// restart clears it, same as the failed-login limiter: the caller simply has
// to log in again, which is acceptable for a single-process self-hosted
// server.
type challengeStore struct {
	mu      sync.Mutex
	now     func() time.Time
	max     int                        // capacity; production always uses maxChallenges, tests may shrink it
	entries map[string]*challengeEntry // key: SHA-256 hex of the raw challenge
}

func newChallengeStore(now func() time.Time) *challengeStore {
	return &challengeStore{now: now, max: maxChallenges, entries: map[string]*challengeEntry{}}
}

// insert creates a new challenge for userID and returns its raw value (to be
// sent to the caller) and expiry. It cleans up expired challenges first, and
// if the store is still at capacity, evicts the entry closest to expiring to
// make room.
func (c *challengeStore) insert(userID int64, remember bool) (string, time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	c.pruneLocked(now)
	if len(c.entries) >= c.max {
		c.evictOldestLocked()
	}
	token := newSecret()
	expiresAt := now.Add(challengeTTL)
	c.entries[hashToken(token)] = &challengeEntry{userID: userID, remember: remember, expiresAt: expiresAt}
	return token, expiresAt
}

// get returns a copy of the live entry for token, or false if it does not
// exist or has expired (an expired entry is dropped as a side effect).
func (c *challengeStore) get(token string) (challengeEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := hashToken(token)
	e, ok := c.entries[key]
	if !ok {
		return challengeEntry{}, false
	}
	if c.now().After(e.expiresAt) {
		delete(c.entries, key)
		return challengeEntry{}, false
	}
	return *e, true
}

// delete removes token, if present.
func (c *challengeStore) delete(token string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, hashToken(token))
}

// fail records one more failed attempt against token and returns the new
// attempts count. It is a no-op returning 0 if token is not (or is no
// longer) a live challenge.
func (c *challengeStore) fail(token string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[hashToken(token)]
	if !ok {
		return 0
	}
	e.attempts++
	return e.attempts
}

func (c *challengeStore) pruneLocked(now time.Time) {
	for k, e := range c.entries {
		if now.After(e.expiresAt) {
			delete(c.entries, k)
		}
	}
}

func (c *challengeStore) evictOldestLocked() {
	var oldestKey string
	var oldest time.Time
	for k, e := range c.entries {
		if oldestKey == "" || e.expiresAt.Before(oldest) {
			oldestKey, oldest = k, e.expiresAt
		}
	}
	if oldestKey != "" {
		delete(c.entries, oldestKey)
	}
}
