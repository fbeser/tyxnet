//go:build darwin

package darwin

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"strings"

	"github.com/fbeser/tyxnet/internal/tunnel"
)

type Factory struct{}

func (Factory) Open(_ context.Context, _ string, mtu int) (tunnel.Device, error) {
	return tunnel.OpenNative("utun", mtu)
}

// Ensure asks the kernel for an utunN interface and configures it. macOS does
// not allow arbitrary utun names; the interface vanishes when its owner closes.
func Ensure(ctx context.Context, name, addressCIDR string, mtu int) (tunnel.Device, error) {
	_ = name
	ip, n, err := net.ParseCIDR(addressCIDR)
	if err != nil {
		return nil, err
	}
	parts := make([]string, len(n.Mask))
	for i, b := range n.Mask {
		parts[i] = strconv.Itoa(int(b))
	}
	mask := strings.Join(parts, ".")
	d, err := (Factory{}).Open(ctx, "utun", mtu)
	if err != nil {
		return nil, fmt.Errorf("open macOS utun (run as root): %w", err)
	}
	args := []string{d.Name(), "inet", ip.String(), ip.String(), "netmask", mask, "mtu", strconv.Itoa(mtu), "up"}
	if out, runErr := exec.CommandContext(ctx, "/sbin/ifconfig", args...).CombinedOutput(); runErr != nil {
		_ = d.Close()
		return nil, fmt.Errorf("configure macOS utun: %w: %s", runErr, string(out))
	}
	return d, nil
}
