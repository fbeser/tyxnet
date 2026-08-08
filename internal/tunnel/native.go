package tunnel

import (
	"errors"
	"fmt"
	"sync"

	nativetun "golang.zx2c4.com/wireguard/tun"
)

// Native wraps wireguard-go's audited OS adapter package. TyxNet uses only the
// TUN implementation and does not use the WireGuard protocol or server.
type Native struct {
	device      nativetun.Device
	name        string
	readMu      sync.Mutex
	readBuffer  []byte
	writeMu     sync.Mutex
	writeBuffer []byte
}

// nativePacketOffset leaves room for the platform packet header used by utun
// on macOS and BSD. The wireguard TUN implementations on other platforms also
// honor the offset and expose only the IP packet to callers.
const nativePacketOffset = 4

func OpenNative(name string, mtu int) (*Native, error) {
	d, err := nativetun.CreateTUN(name, mtu)
	if err != nil {
		return nil, fmt.Errorf("create native TUN %q: %w", name, err)
	}
	actual, err := d.Name()
	if err != nil {
		_ = d.Close()
		return nil, fmt.Errorf("get TUN name: %w", err)
	}
	return &Native{device: d, name: actual}, nil
}
func (n *Native) Name() string { return n.name }
func (n *Native) Close() error { return n.device.Close() }
func (n *Native) Read(p []byte) (int, error) {
	n.readMu.Lock()
	defer n.readMu.Unlock()
	if cap(n.readBuffer) < nativePacketOffset+len(p) {
		n.readBuffer = make([]byte, nativePacketOffset+len(p))
	}
	buffer := n.readBuffer[:nativePacketOffset+len(p)]
	sizes := []int{0}
	count, err := n.device.Read([][]byte{buffer}, sizes, nativePacketOffset)
	if err != nil {
		return 0, err
	}
	if count == 0 {
		return 0, nil
	}
	if sizes[0] < 0 || sizes[0] > len(p) {
		return 0, errors.New("native TUN returned invalid packet size")
	}
	copy(p, buffer[nativePacketOffset:nativePacketOffset+sizes[0]])
	return sizes[0], nil
}
func (n *Native) Write(p []byte) (int, error) {
	n.writeMu.Lock()
	defer n.writeMu.Unlock()
	if cap(n.writeBuffer) < nativePacketOffset+len(p) {
		n.writeBuffer = make([]byte, nativePacketOffset+len(p))
	}
	buffer := n.writeBuffer[:nativePacketOffset+len(p)]
	copy(buffer[nativePacketOffset:], p)
	count, err := n.device.Write([][]byte{buffer}, nativePacketOffset)
	if err != nil {
		return 0, err
	}
	if count != 1 {
		return 0, errors.New("native TUN did not write packet")
	}
	return len(p), nil
}
