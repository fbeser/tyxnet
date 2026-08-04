package tunnel

import (
	"context"
	"errors"
)

type Device interface {
	Read([]byte) (int, error)
	Write([]byte) (int, error)
	Close() error
	Name() string
}
type Factory interface {
	Open(context.Context, string, int) (Device, error)
}

var ErrNotImplemented = errors.New("TUN adapter is not implemented on this platform")
