package edition

import "crypto/ed25519"

// publicKey checks the licenses the issuer signs (kept license issue). Its
// private half is kept by the issuer, never in this repository.
var publicKey = ed25519.PublicKey(mustKey("z4dhA86tIx3YjG4WQYLnl7wEe6nfkgj1wEgYAkN-F5E"))

func mustKey(s string) []byte {
	if s == "" {
		return nil
	}
	k, err := DecodeKey(s)
	if err != nil || len(k) != ed25519.PublicKeySize {
		panic("edition: bad public key")
	}
	return k
}
