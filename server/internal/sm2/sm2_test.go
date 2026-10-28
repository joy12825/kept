package sm2

import (
	"encoding/hex"
	"testing"
)

// Made with OpenSSL 3: a key, and the message signed with the default ID
// (-sigopt distid:1234567812345678) and with an empty one.
var (
	openSSLKey     = "04fb1ca477c6e1827df24545e91d333eaa8f4fbf2e74479382baad1b080485cb89521c03029c3d47cf368e27b539e8383d3b8239392af3b59d990612dc471fa10a"
	openSSLMsg     = "电子发票签章测试"
	openSSLDefault = "304602210082af7c7f9393f6c7c65ae5756b9cad9923cf77eed3fd61c2ec5c11970c0cb744022100d77e192912e5921dcaca210bc28f347ba42913b107880ecf914ff9bcf8acc163"
	openSSLEmptyID = "3045022100d0bd9ce559ea8cf4305b76718518bd91b034d238eca7969d8fdbc2e45dadb067022004bb6f972f87ab5243698b5ded23a9a9a83aaf9786e124cea1c423687e3e782d"
)

func unhex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestVerifyOpenSSL(t *testing.T) {
	pub, err := ParsePublicKey(unhex(t, openSSLKey))
	if err != nil {
		t.Fatal(err)
	}
	msg := []byte(openSSLMsg)
	if !VerifyASN1(pub, DefaultID, msg, unhex(t, openSSLDefault)) {
		t.Error("the default ID's signature")
	}
	if !VerifyASN1(pub, nil, msg, unhex(t, openSSLEmptyID)) {
		t.Error("the empty ID's signature")
	}
	if VerifyASN1(pub, DefaultID, msg, unhex(t, openSSLEmptyID)) {
		t.Error("a signature with another ID verified")
	}
	if VerifyASN1(pub, DefaultID, []byte(openSSLMsg+"。"), unhex(t, openSSLDefault)) {
		t.Error("another message verified")
	}
	bad := unhex(t, openSSLDefault)
	bad[len(bad)-1] ^= 1
	if VerifyASN1(pub, DefaultID, msg, bad) {
		t.Error("a changed signature verified")
	}
	if VerifyASN1(pub, DefaultID, msg, []byte("not a signature")) {
		t.Error("garbage verified")
	}
}

func TestSignAndVerify(t *testing.T) {
	k, err := GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParsePublicKey(k.Bytes()); err != nil {
		t.Fatal("the key is not on the curve")
	}
	sig, err := SignASN1(nil, k, DefaultID, []byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyASN1(&k.PublicKey, DefaultID, []byte("hello"), sig) {
		t.Error("own signature")
	}
	other, _ := GenerateKey(nil)
	if VerifyASN1(&other.PublicKey, DefaultID, []byte("hello"), sig) {
		t.Error("another key verified")
	}
	if _, err := ParsePublicKey(append([]byte{4}, make([]byte, 64)...)); err == nil {
		t.Error("a point off the curve")
	}
}
