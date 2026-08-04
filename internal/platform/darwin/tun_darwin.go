//go:build darwin

// Package darwin defines the macOS virtual-adapter integration boundary.
package darwin

import (
	"context"

	"github.com/fbeser/tyxnet/internal/tunnel"
)

// Factory will eventually create an utun interface or bridge to a separately
// signed Network Extension. The first milestone intentionally reports that the
// adapter is unavailable instead of claiming a working tunnel.
type Factory struct{}

func (Factory) Open(context.Context, string, int) (tunnel.Device, error) {
	return nil, tunnel.ErrNotImplemented
}
