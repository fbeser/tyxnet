package crypto

import (
	"bytes"
	"testing"
)

func TestCipherAndReplay(t *testing.T) {
	key := make([]byte, 32)
	c, _ := NewCipher(key)
	sealed := c.Seal(1, 1, []byte("secret"), []byte("header"))
	got, err := c.Open(1, 1, sealed, []byte("header"))
	if err != nil || string(got) != "secret" {
		t.Fatalf("open: %q %v", got, err)
	}
	if _, err := c.Open(1, 1, sealed, []byte("header")); err == nil {
		t.Fatal("replay accepted")
	}
}
func TestBadAuthTag(t *testing.T) {
	c, _ := NewCipher(make([]byte, 32))
	b := c.Seal(1, 2, []byte("x"), nil)
	b[0] ^= 1
	if _, err := c.Open(1, 2, b, nil); err == nil {
		t.Fatal("bad tag accepted")
	}
}

func TestDirectionalDataKeysAreSeparated(t *testing.T) {
	secret := make([]byte, DataKeySize)
	for index := range secret {
		secret[index] = byte(index + 1)
	}
	clientToServer, err := DeriveDirectionalKey(secret, 42, "client-to-server")
	if err != nil {
		t.Fatal(err)
	}
	serverToClient, err := DeriveDirectionalKey(secret, 42, "server-to-client")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(clientToServer, serverToClient) {
		t.Fatal("directional keys must differ")
	}
	if _, err := DeriveDirectionalKey(secret, 42, "invalid"); err == nil {
		t.Fatal("invalid direction was accepted")
	}
	if _, err := DeriveDirectionalKey(secret, 0, "client-to-server"); err == nil {
		t.Fatal("zero session was accepted")
	}
}
