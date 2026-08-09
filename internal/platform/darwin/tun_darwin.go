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

type fixedCommand struct {
	path string
	args []string
}

func (Factory) Open(_ context.Context, _ string, mtu int) (tunnel.Device, error) {
	return tunnel.OpenNative("utun", mtu)
}

// Ensure asks the kernel for an utunN interface and configures its address,
// MTU, and virtual-network route. macOS does not allow arbitrary utun names;
// the interface and its scoped route vanish when its owner closes.
func Ensure(ctx context.Context, name, addressCIDR string, mtu int) (tunnel.Device, error) {
	_ = name
	ip, network, err := net.ParseCIDR(addressCIDR)
	if err != nil {
		return nil, err
	}
	parts := make([]string, len(network.Mask))
	for i, b := range network.Mask {
		parts[i] = strconv.Itoa(int(b))
	}
	mask := strings.Join(parts, ".")
	d, err := (Factory{}).Open(ctx, "utun", mtu)
	if err != nil {
		return nil, fmt.Errorf("open macOS utun (run as root): %w", err)
	}
	commands := darwinTunnelCommands(d.Name(), ip, network, mask, mtu)
	for _, command := range commands {
		if out, runErr := exec.CommandContext(ctx, command.path, command.args...).CombinedOutput(); runErr != nil {
			_ = d.Close()
			return nil, fmt.Errorf("configure macOS utun with %s: %w: %s", command.path, runErr, string(out))
		}
	}
	return d, nil
}

func darwinTunnelCommands(name string, ip net.IP, network *net.IPNet, mask string, mtu int) []fixedCommand {
	return []fixedCommand{
		{path: "/sbin/ifconfig", args: []string{name, "inet", ip.String(), ip.String(), "netmask", mask, "mtu", strconv.Itoa(mtu), "up"}},
		{path: "/sbin/route", args: []string{"-n", "add", "-net", network.String(), "-interface", name}},
	}
}
