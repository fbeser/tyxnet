//go:build windows

package windows

import (
	"context"
	"github.com/fbeser/tyxnet/internal/tunnel"
)

// Factory is the integration boundary for Wintun. Driver redistribution and
// adapter creation are intentionally not implemented in this first milestone.
type Factory struct{}

func (Factory) Open(context.Context, string, int) (tunnel.Device, error) {
	return nil, tunnel.ErrNotImplemented
}
