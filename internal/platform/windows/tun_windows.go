//go:build windows

package windows

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"strings"

	"github.com/fbeser/tyxnet/internal/tunnel"
	"golang.org/x/sys/windows"
	nativetun "golang.zx2c4.com/wireguard/tun"
)

// A stable adapter GUID keeps Windows Network Location Awareness from creating
// TyxNet 2, TyxNet 3, and later profiles on every server restart.
var tyxNetAdapterGUID = windows.GUID{
	Data1: 0x7f236a91,
	Data2: 0x99e4,
	Data3: 0x54bd,
	Data4: [8]byte{0xa8, 0x15, 0x65, 0xf4, 0xf5, 0xab, 0x28, 0x30},
}

type Factory struct{}

func (Factory) Open(_ context.Context, name string, mtu int) (tunnel.Device, error) {
	nativetun.WintunTunnelType = "TyxNet"
	guid := adapterGUID(name)
	nativetun.WintunStaticRequestedGUID = &guid
	return tunnel.OpenNative(name, mtu)
}

func adapterGUID(name string) windows.GUID {
	if strings.EqualFold(name, "TyxNet") {
		return tyxNetAdapterGUID
	}
	sum := sha256.Sum256([]byte("github.com/fbeser/tyxnet/wintun/" + strings.ToLower(name)))
	sum[6] = (sum[6] & 0x0f) | 0x50
	sum[8] = (sum[8] & 0x3f) | 0x80
	return windows.GUID{
		Data1: binary.BigEndian.Uint32(sum[0:4]),
		Data2: binary.BigEndian.Uint16(sum[4:6]),
		Data3: binary.BigEndian.Uint16(sum[6:8]),
		Data4: [8]byte(sum[8:16]),
	}
}

// Ensure reuses a Wintun adapter with the same name when one already exists and
// idempotently applies the server address and MTU. It requires elevation and a
// verified wintun.dll beside the executable.
func Ensure(ctx context.Context, name, addressCIDR string, mtu int) (tunnel.Device, error) {
	ip, n, err := net.ParseCIDR(addressCIDR)
	if err != nil {
		return nil, err
	}
	mask := net.IP(n.Mask).String()
	if mask == "<nil>" {
		parts := make([]string, len(n.Mask))
		for i, b := range n.Mask {
			parts[i] = strconv.Itoa(int(b))
		}
		mask = strings.Join(parts, ".")
	}
	d, err := (Factory{}).Open(ctx, name, mtu)
	if err != nil {
		return nil, fmt.Errorf("open Wintun adapter (run elevated and install wintun.dll): %w", err)
	}
	commands := [][]string{{"interface", "ipv4", "set", "address", "name=" + d.Name(), "source=static", "address=" + ip.String(), "mask=" + mask, "gateway=none", "store=active"}, {"interface", "ipv4", "set", "subinterface", d.Name(), "mtu=" + strconv.Itoa(mtu), "store=active"}}
	for _, args := range commands {
		if out, runErr := exec.CommandContext(ctx, "netsh", args...).CombinedOutput(); runErr != nil {
			_ = d.Close()
			return nil, fmt.Errorf("configure Wintun with netsh %v: %w: %s", args, runErr, string(out))
		}
	}
	return d, nil
}
