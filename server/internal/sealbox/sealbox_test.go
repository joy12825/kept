package sealbox

import (
	"bytes"
	"crypto/rand"
	"errors"
	"io"
	"testing"
)

func TestStreamsOfEverySize(t *testing.T) {
	k, err := NewKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range []int{0, 1, chunkSize - 1, chunkSize, chunkSize + 1, 3*chunkSize + 17} {
		plain := make([]byte, n)
		_, _ = rand.Read(plain)
		var b bytes.Buffer
		w, err := Encrypt(&b, k.Public)
		if err != nil {
			t.Fatal(err)
		}
		// written in odd pieces
		for p := plain; len(p) > 0; {
			m := min(len(p), 1000)
			if _, err := w.Write(p[:m]); err != nil {
				t.Fatal(err)
			}
			p = p[m:]
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		sealed := b.Bytes()
		r, err := Decrypt(bytes.NewReader(sealed), k.Private)
		if err != nil {
			t.Fatal(err)
		}
		got, err := io.ReadAll(r)
		if err != nil || !bytes.Equal(got, plain) {
			t.Fatalf("%d bytes: %v (got %d)", n, err, len(got))
		}
		// cut short at a chunk boundary, or changed: refused
		if n > chunkSize {
			cut := sealed[:len(magic)+32+chunkSize+tagSize]
			r, _ := Decrypt(bytes.NewReader(cut), k.Private)
			if _, err := io.ReadAll(r); !errors.Is(err, ErrWrongKey) {
				t.Errorf("%d bytes cut short: %v", n, err)
			}
		}
		bad := bytes.Clone(sealed)
		bad[len(bad)-1] ^= 1
		r, _ = Decrypt(bytes.NewReader(bad), k.Private)
		if _, err := io.ReadAll(r); !errors.Is(err, ErrWrongKey) {
			t.Errorf("%d bytes changed: %v", n, err)
		}
	}
}

func TestOtherKeysAndPlainFiles(t *testing.T) {
	a, _ := NewKeyPair()
	b, _ := NewKeyPair()
	s, err := SealString(a.Public, "2023年度体检报告")
	if err != nil || !IsSealedString(s) {
		t.Fatal(s, err)
	}
	if _, err := OpenString(b.Private, s); !errors.Is(err, ErrWrongKey) {
		t.Errorf("other key: %v", err)
	}
	if got, err := OpenString(a.Private, s); err != nil || got != "2023年度体检报告" {
		t.Errorf("open: %q %v", got, err)
	}
	if got, _ := OpenString(a.Private, "电费"); got != "电费" {
		t.Errorf("plain value: %q", got)
	}
	if _, err := Decrypt(bytes.NewReader([]byte("%PDF-1.4 hello")), a.Private); !errors.Is(err, ErrNotSealed) {
		t.Errorf("plain file: %v", err)
	}
}

func TestWrappedKeys(t *testing.T) {
	k, _ := NewKeyPair()
	code, err := NewRecoveryCode()
	if err != nil || len(code) != 29 {
		t.Fatal(code, err)
	}
	w, err := Wrap(code, k.Private)
	if err != nil {
		t.Fatal(err)
	}
	// typed loosely
	typed := NormalizeCode(" " + string(bytes.ToLower([]byte(code[:5]))) + code[6:] + " ")
	got, err := Unwrap(typed, w)
	if err != nil || !bytes.Equal(got, k.Private) {
		t.Fatalf("unwrap %q: %v", typed, err)
	}
	if pub, _ := PublicOf(got); !bytes.Equal(pub, k.Public) {
		t.Error("public key does not match")
	}
	if _, err := Unwrap("wrong", w); !errors.Is(err, ErrWrongCode) {
		t.Errorf("wrong code: %v", err)
	}
}
