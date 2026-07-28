//go:build windows

package profile

import (
	"bytes"
	"testing"
)

func TestDPAPIRoundTripAndEntropy(t *testing.T) {
	plain := []byte(`{"user":"test","password":"secret"}`)
	cipher, err := ProtectCurrentUser(plain, "profile-a")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(cipher, []byte("secret")) {
		t.Fatal("ciphertext contains plaintext secret")
	}
	got, err := UnprotectCurrentUser(cipher, "profile-a")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, plain) {
		t.Fatal("round-trip mismatch")
	}
	if _, err := UnprotectCurrentUser(cipher, "profile-b"); err == nil {
		t.Fatal("different profile entropy must fail")
	}
}
