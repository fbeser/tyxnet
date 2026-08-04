//go:build windows

package platform

import (
	"context"
	"github.com/fbeser/tyxnet/internal/platform/windows"
	"github.com/fbeser/tyxnet/internal/tunnel"
)

func EnsureTunnel(ctx context.Context, name, addressCIDR string, mtu int) (tunnel.Device, error) {
	return windows.Ensure(ctx, name, addressCIDR, mtu)
}
