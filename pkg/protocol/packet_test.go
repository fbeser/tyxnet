package protocol

import "testing"

func TestPacketRoundTrip(t *testing.T) {
	want := Packet{Type: TypeData, NetworkID: 3, SessionID: 4, SourceID: 5, DestinationID: 6, Sequence: 7, Payload: []byte("packet")}
	b, err := want.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParsePacket(b)
	if err != nil {
		t.Fatal(err)
	}
	if got.Sequence != want.Sequence || string(got.Payload) != "packet" || got.DestinationID != 6 {
		t.Fatalf("unexpected packet: %+v", got)
	}
}
func TestPacketRejectsMalformed(t *testing.T) {
	if _, err := ParsePacket([]byte("bad")); err == nil {
		t.Fatal("expected error")
	}
}
