package dataplane

import (
	"encoding/binary"
	"errors"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/fbeser/tyxnet/internal/routing"
)

type testTunnel struct {
	name     string
	outbound chan []byte
	inbound  chan []byte
	closed   chan struct{}
}

func newTestTunnel(name string) *testTunnel {
	return &testTunnel{name: name, outbound: make(chan []byte, 8), inbound: make(chan []byte, 8), closed: make(chan struct{})}
}

func (t *testTunnel) Name() string { return t.name }
func (t *testTunnel) Read(buffer []byte) (int, error) {
	select {
	case packet := <-t.outbound:
		return copy(buffer, packet), nil
	case <-t.closed:
		return 0, errors.New("closed")
	}
}
func (t *testTunnel) Write(packet []byte) (int, error) {
	copyPacket := append([]byte(nil), packet...)
	select {
	case t.inbound <- copyPacket:
		return len(packet), nil
	case <-t.closed:
		return 0, errors.New("closed")
	}
}
func (t *testTunnel) Close() error {
	select {
	case <-t.closed:
	default:
		close(t.closed)
	}
	return nil
}

func TestEncryptedUDPDataPlaneRoutesClientsAndServer(t *testing.T) {
	serverTunnel := newTestTunnel("server")
	monitor := routing.NewTrafficMonitor()
	server, err := Listen("127.0.0.1:0", serverTunnel, net.ParseIP("10.90.0.1"), 1280, monitor)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = server.Close()
		_ = serverTunnel.Close()
	}()

	firstTunnel := newTestTunnel("first")
	secondTunnel := newTestTunnel("second")
	first := configureTestClient(t, server, firstTunnel, "dev_first", "10.90.0.2")
	second := configureTestClient(t, server, secondTunnel, "dev_second", "10.90.0.3")
	defer func() {
		_ = first.Close()
		_ = second.Close()
		_ = firstTunnel.Close()
		_ = secondTunnel.Close()
	}()

	clientPacket := ipv4Packet(net.ParseIP("10.90.0.2"), net.ParseIP("10.90.0.3"), []byte("client-to-client"))
	firstTunnel.outbound <- clientPacket
	if received := receivePacket(t, secondTunnel.inbound); string(received[20:]) != "client-to-client" {
		t.Fatalf("unexpected client packet: %x", received)
	}

	serverPacket := ipv4Packet(net.ParseIP("10.90.0.2"), net.ParseIP("10.90.0.1"), []byte("client-to-server"))
	firstTunnel.outbound <- serverPacket
	if received := receivePacket(t, serverTunnel.inbound); string(received[20:]) != "client-to-server" {
		t.Fatalf("unexpected server packet: %x", received)
	}

	response := ipv4Packet(net.ParseIP("10.90.0.1"), net.ParseIP("10.90.0.2"), []byte("server-response"))
	serverTunnel.outbound <- response
	if received := receivePacket(t, firstTunnel.inbound); string(received[20:]) != "server-response" {
		t.Fatalf("unexpected server response: %x", received)
	}

	spoofed := ipv4Packet(net.ParseIP("10.90.0.99"), net.ParseIP("10.90.0.3"), []byte("spoofed"))
	firstTunnel.outbound <- spoofed
	select {
	case packet := <-secondTunnel.inbound:
		t.Fatalf("spoofed packet was routed: %x", packet)
	case <-time.After(100 * time.Millisecond):
	}

	snapshot := monitor.Snapshot()
	if !snapshot.Ready || snapshot.Packets != 3 {
		t.Fatalf("unexpected traffic snapshot: %+v", snapshot)
	}
}

func TestInvalidUDPSourceIsRateLimited(t *testing.T) {
	server := &Server{invalid: map[string]invalidSource{}}
	source := net.ParseIP("192.0.2.10")
	for count := 0; count < 120; count++ {
		server.recordInvalid(source)
	}
	if !server.invalidBlocked(source) {
		t.Fatal("invalid UDP source was not blocked")
	}
	if server.invalidBlocked(net.ParseIP("192.0.2.11")) {
		t.Fatal("unrelated UDP source was blocked")
	}
}

func configureTestClient(t *testing.T, server *Server, adapter *testTunnel, deviceID, assignedIP string) *Client {
	t.Helper()
	bootstrap, err := server.Register(deviceID, net.ParseIP(assignedIP))
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewClient(adapter, net.ParseIP(assignedIP), deviceID, 1280)
	if err != nil {
		t.Fatal(err)
	}
	endpoint := net.JoinHostPort("127.0.0.1", strconv.Itoa(server.Port()))
	if err := client.Configure("https://vpn.example.com", endpoint, bootstrap); err != nil {
		_ = client.Close()
		t.Fatal(err)
	}
	return client
}

func receivePacket(t *testing.T, packets <-chan []byte) []byte {
	t.Helper()
	select {
	case packet := <-packets:
		return packet
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for routed packet")
		return nil
	}
}

func ipv4Packet(source, destination net.IP, payload []byte) []byte {
	packet := make([]byte, 20+len(payload))
	packet[0] = 0x45
	binary.BigEndian.PutUint16(packet[2:4], uint16(len(packet)))
	copy(packet[12:16], source.To4())
	copy(packet[16:20], destination.To4())
	copy(packet[20:], payload)
	return packet
}
