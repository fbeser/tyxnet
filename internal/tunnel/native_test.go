package tunnel

import (
	"bytes"
	"errors"
	"os"
	"testing"

	nativetun "golang.zx2c4.com/wireguard/tun"
)

type nativeDeviceStub struct {
	readPacket  []byte
	writePacket []byte
	readOffset  int
	writeOffset int
}

func (d *nativeDeviceStub) File() *os.File                 { return nil }
func (d *nativeDeviceStub) MTU() (int, error)              { return 1280, nil }
func (d *nativeDeviceStub) Name() (string, error)          { return "test0", nil }
func (d *nativeDeviceStub) Events() <-chan nativetun.Event { return nil }
func (d *nativeDeviceStub) Close() error                   { return nil }
func (d *nativeDeviceStub) BatchSize() int                 { return 1 }
func (d *nativeDeviceStub) Read(bufs [][]byte, sizes []int, offset int) (int, error) {
	d.readOffset = offset
	if len(bufs) != 1 || len(sizes) != 1 || len(bufs[0]) < offset+len(d.readPacket) {
		return 0, errors.New("invalid read buffers")
	}
	copy(bufs[0][offset:], d.readPacket)
	sizes[0] = len(d.readPacket)
	return 1, nil
}
func (d *nativeDeviceStub) Write(bufs [][]byte, offset int) (int, error) {
	d.writeOffset = offset
	if len(bufs) != 1 || len(bufs[0]) < offset {
		return 0, errors.New("invalid write buffers")
	}
	d.writePacket = append([]byte(nil), bufs[0][offset:]...)
	return 1, nil
}

func TestNativeReservesPlatformPacketHeader(t *testing.T) {
	readPacket := []byte{0x45, 0, 0, 20}
	device := &nativeDeviceStub{readPacket: readPacket}
	native := &Native{device: device, name: "test0"}

	buffer := make([]byte, 1280)
	n, err := native.Read(buffer)
	if err != nil {
		t.Fatalf("read native packet: %v", err)
	}
	if n != len(readPacket) || !bytes.Equal(buffer[:n], readPacket) {
		t.Fatalf("unexpected native packet: size=%d packet=%x", n, buffer[:n])
	}
	if device.readOffset != nativePacketOffset {
		t.Fatalf("read offset = %d, want %d", device.readOffset, nativePacketOffset)
	}

	writePacket := []byte{0x45, 0, 0, 24, 1, 2, 3, 4}
	if _, err := native.Write(writePacket); err != nil {
		t.Fatalf("write native packet: %v", err)
	}
	if device.writeOffset != nativePacketOffset {
		t.Fatalf("write offset = %d, want %d", device.writeOffset, nativePacketOffset)
	}
	if !bytes.Equal(device.writePacket, writePacket) {
		t.Fatalf("unexpected written packet: %x", device.writePacket)
	}
}
