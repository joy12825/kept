package auth

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/binary"
	"fmt"
	"time"
)

// totpStep is the RFC 6238 time step: 30 seconds.
const totpStep = 30 * time.Second

// totpCode computes the RFC 6238 TOTP code (HMAC-SHA1, 6 digits) for secret
// at the given 30-second time step.
func totpCode(secret []byte, step int64) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(step))
	mac := hmac.New(sha1.New, secret)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	code := (uint32(sum[offset])&0x7f)<<24 |
		uint32(sum[offset+1])<<16 |
		uint32(sum[offset+2])<<8 |
		uint32(sum[offset+3])
	return fmt.Sprintf("%06d", code%1000000)
}

// verifyTOTP checks code against secret around now, allowing a drift of one
// time step in either direction. It returns the time step that matched; a
// step must be strictly greater than lastStep to be accepted, which stops a
// used code from being replayed.
func verifyTOTP(secret []byte, code string, now time.Time, lastStep int64) (int64, bool) {
	current := now.Unix() / int64(totpStep/time.Second)
	for _, delta := range [3]int64{0, -1, 1} {
		step := current + delta
		if step <= lastStep {
			continue
		}
		if totpCode(secret, step) == code {
			return step, true
		}
	}
	return 0, false
}
