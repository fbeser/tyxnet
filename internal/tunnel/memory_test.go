package tunnel

import "testing"

func TestMemoryTUN(t *testing.T) {
	m := NewMemory("mock0")
	want := []byte{0x45, 0, 0, 20}
	if _, err := m.Write(want); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, 16)
	n, err := m.Read(got)
	if err != nil || string(got[:n]) != string(want) {
		t.Fatalf("read %v %v", got[:n], err)
	}
}
