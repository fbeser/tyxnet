//go:build linux

package platform

import (
	"context"
	"github.com/fbeser/tyxnet/internal/platform/linux"
	"github.com/fbeser/tyxnet/internal/tunnel"
)

func EnsureTunnel(ctx context.Context, name, addressCIDR string, mtu int) (tunnel.Device, error) {
	return linux.Ensure(ctx, name, addressCIDR, mtu)
}
