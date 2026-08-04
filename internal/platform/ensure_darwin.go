//go:build darwin

package platform

import (
	"context"
	"github.com/fbeser/tyxnet/internal/platform/darwin"
	"github.com/fbeser/tyxnet/internal/tunnel"
)

func EnsureTunnel(ctx context.Context, name, addressCIDR string, mtu int) (tunnel.Device, error) {
	return darwin.Ensure(ctx, name, addressCIDR, mtu)
}
