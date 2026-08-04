package crypto

import "testing"

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
