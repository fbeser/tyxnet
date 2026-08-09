//go:build darwin

package darwin

import (
	"net"
	"reflect"
	"testing"
)

func TestDarwinTunnelCommandsAddVirtualNetworkRoute(t *testing.T) {
	ip, network, err := net.ParseCIDR("10.90.0.3/24")
	if err != nil {
		t.Fatalf("parse test network: %v", err)
	}
	commands := darwinTunnelCommands("utun7", ip, network, "255.255.255.0", 1280)
	want := []fixedCommand{
		{path: "/sbin/ifconfig", args: []string{"utun7", "inet", "10.90.0.3", "10.90.0.3", "netmask", "255.255.255.0", "mtu", "1280", "up"}},
		{path: "/sbin/route", args: []string{"-n", "add", "-net", "10.90.0.0/24", "-interface", "utun7"}},
	}
	if !reflect.DeepEqual(commands, want) {
		t.Fatalf("unexpected macOS tunnel commands:\n got: %#v\nwant: %#v", commands, want)
	}
}
