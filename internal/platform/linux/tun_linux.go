//go:build linux

package linux

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"

	"github.com/fbeser/tyxnet/internal/tunnel"
)

type Factory struct{}

func (Factory) Open(_ context.Context, name string, mtu int) (tunnel.Device, error) {
	return tunnel.OpenNative(name, mtu)
}

// Ensure opens or attaches to the named TUN and idempotently replaces its
// address and link state. The descriptor keeps the non-persistent TUN alive.
func Ensure(ctx context.Context, name, addressCIDR string, mtu int) (tunnel.Device, error) {
	d, err := (Factory{}).Open(ctx, name, mtu)
	if err != nil {
		return nil, err
	}
	actual := d.Name()
	commands := [][]string{{"link", "set", "dev", actual, "mtu", strconv.Itoa(mtu), "up"}, {"address", "replace", addressCIDR, "dev", actual}}
	for _, args := range commands {
		if out, runErr := exec.CommandContext(ctx, "ip", args...).CombinedOutput(); runErr != nil {
			_ = d.Close()
			return nil, fmt.Errorf("configure Linux TUN with ip %v: %w: %s", args, runErr, string(out))
		}
	}
	return d, nil
}
