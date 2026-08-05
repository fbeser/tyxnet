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
	observed := 0
	r := NewObserved(func(_, _ net.IP, packetBytes int) { observed += packetBytes })
	s := &sink{}
	r.Add(net.ParseIP("10.90.0.3"), s)
	if err := r.Route(net.ParseIP("10.90.0.2"), packet(net.ParseIP("10.90.0.2"), net.ParseIP("10.90.0.3"))); err != nil {
		t.Fatal(err)
	}
	if len(s.b) == 0 {
		t.Fatal("not routed")
	}
	if observed != len(s.b) {
		t.Fatalf("observed bytes = %d, want %d", observed, len(s.b))
	}
	if err := r.Route(net.ParseIP("10.90.0.9"), packet(net.ParseIP("10.90.0.2"), net.ParseIP("10.90.0.3"))); err == nil {
		t.Fatal("spoof accepted")
	}
	if observed != len(s.b) {
		t.Fatal("rejected packet was included in traffic telemetry")
	}
}
