package sm3

import (
	"encoding/hex"
	"strings"
	"testing"
)

func TestSum(t *testing.T) {
	long := make([]byte, 100003)
	for i := range long {
		long[i] = byte(i % 251)
	}
	for in, want := range map[string]string{
		// GB/T 32905-2016, appendix A
		"abc":                      "66c7f0f462eeedd9d1f2d46bdc10e4e24167c4875cf2f7a2297da02b8f4ba8e0",
		strings.Repeat("abcd", 16): "debe9ff92275b8a138604889c18e5a4d6fdb70e5387e5765293dcba39c0c5732",
		// computed with OpenSSL 3
		string(long): "f992ae0e2e0ef40c639ea48e2707423450d6b80e5872914aba3d6ff6be30c1dd",
		"":           "1ab21d8355cfa17f8e61194831e81a8f22bec8c728fefb747ed035eb5082aa2b",
	} {
		got := Sum([]byte(in))
		if hex.EncodeToString(got[:]) != want {
			t.Errorf("SM3 of %d bytes = %x", len(in), got)
		}
		// written in pieces, with Sum in between
		h := New()
		for i := 0; i < len(in); i += 37 {
			h.Write([]byte(in[i:min(i+37, len(in))]))
			h.Sum(nil)
		}
		if hex.EncodeToString(h.Sum(nil)) != want {
			t.Errorf("SM3 in pieces of %d bytes", len(in))
		}
	}
}
