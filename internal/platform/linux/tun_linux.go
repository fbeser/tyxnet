//go:build linux

package linux

import (
	"context"
	"fmt"
	"os"
	"syscall"
	"unsafe"

	"github.com/fbeser/tyxnet/internal/tunnel"
)

const (
	tunSetIFF = 0x400454ca
	iffTUN    = 0x0001
	iffNoPI   = 0x1000
)

type Factory struct{}
type device struct {
	*os.File
	name string
}

func (d *device) Name() string { return d.name }
func (Factory) Open(_ context.Context, name string, _ int) (tunnel.Device, error) {
	f, err := os.OpenFile("/dev/net/tun", os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("open TUN: %w", err)
	}
	var req [40]byte
	copy(req[:15], name)
	*(*uint16)(unsafe.Pointer(&req[16])) = iffTUN | iffNoPI
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), tunSetIFF, uintptr(unsafe.Pointer(&req[0])))
	if errno != 0 {
		f.Close()
		return nil, fmt.Errorf("configure TUN: %w", errno)
	}
	actual := string(req[:])
	for i, c := range actual {
		if c == 0 {
			actual = actual[:i]
			break
		}
	}
	return &device{File: f, name: actual}, nil
}
