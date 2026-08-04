package routing

import (
	"net"
	"testing"
)

type sink struct{ b []byte }

func (s *sink) Send(b []byte) error { s.b = b; return nil }
func packet(src, dst net.IP) []byte {
	b := make([]byte, 20)
	b[0] = 0x45
	copy(b[12:16], src.To4())
	copy(b[16:20], dst.To4())
	return b
}
func TestRoutingAndSpoofing(t *testing.T) {
	r := New()
	s := &sink{}
	r.Add(net.ParseIP("10.90.0.3"), s)
	if err := r.Route(net.ParseIP("10.90.0.2"), packet(net.ParseIP("10.90.0.2"), net.ParseIP("10.90.0.3"))); err != nil {
		t.Fatal(err)
	}
	if len(s.b) == 0 {
		t.Fatal("not routed")
	}
	if err := r.Route(net.ParseIP("10.90.0.9"), packet(net.ParseIP("10.90.0.2"), net.ParseIP("10.90.0.3"))); err == nil {
		t.Fatal("spoof accepted")
	}
}
