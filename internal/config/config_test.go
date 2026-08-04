package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestServerValidation(t *testing.T) {
	c := DefaultServer()
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	c.Network = "10.0.0.1/32"
	if err := c.Validate(); err == nil {
		t.Fatal("expected invalid network")
	}
}

func TestWebInterfacesDefaultToLAN(t *testing.T) {
	server := DefaultServer()
	if server.ListenAddress != "0.0.0.0" || !server.AllowRemoteSetup || !server.AllowInsecureHTTP {
		t.Fatalf("unexpected server web defaults: %+v", server)
	}
	client := DefaultClient()
	if client.LocalAddress != "0.0.0.0:9070" || !client.AllowRemoteUI {
		t.Fatalf("unexpected client web defaults: %+v", client)
	}
}
func TestServerRejectsPublicPlainHTTP(t *testing.T) {
	c := DefaultServer()
	c.ListenAddress = "0.0.0.0"
	c.AllowInsecureHTTP = false
	if err := c.Validate(); err == nil {
		t.Fatal("expected public plaintext bind to fail")
	}
	c.AllowInsecureHTTP = true
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestServerTunnelValidation(t *testing.T) {
	tests := []struct {
		name   string
		change func(*Server)
	}{
		{"empty name", func(c *Server) { c.TunnelName = "" }},
		{"long name", func(c *Server) { c.TunnelName = "this-name-is-too-long" }},
		{"address outside network", func(c *Server) { c.TunnelAddress = "10.91.0.1" }},
		{"network address", func(c *Server) { c.TunnelAddress = "10.90.0.0" }},
		{"broadcast address", func(c *Server) { c.TunnelAddress = "10.90.0.255" }},
		{"small MTU", func(c *Server) { c.TunnelMTU = 575 }},
		{"large MTU", func(c *Server) { c.TunnelMTU = 9001 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := DefaultServer()
			tt.change(&c)
			if err := c.Validate(); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}

	c := DefaultServer()
	c.TunnelEnabled = false
	c.TunnelName = ""
	c.TunnelAddress = ""
	c.TunnelMTU = 0
	if err := c.Validate(); err != nil {
		t.Fatalf("disabled tunnel should not require adapter settings: %v", err)
	}
}

func TestClientLocalAPIMustBeLoopback(t *testing.T) {
	c := DefaultClient()
	c.ServerURL = "https://example.test"
	c.Name = "node"
	c.LocalAddress = "0.0.0.0:9070"
	c.AllowRemoteUI = false
	if err := c.Validate(); err == nil {
		t.Fatal("expected unsafe bind to fail")
	}
	c.AllowRemoteUI = true
	if err := c.Validate(); err != nil {
		t.Fatalf("explicit remote UI should validate: %v", err)
	}
}

func TestClientTunnelValidation(t *testing.T) {
	c := DefaultClient()
	c.ServerURL = "https://example.test"
	c.Name = "node"
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	c.TunnelMTU = 575
	if err := c.Validate(); err == nil {
		t.Fatal("expected invalid client tunnel MTU")
	}
	c.TunnelEnabled = false
	c.TunnelMTU = 0
	if err := c.Validate(); err != nil {
		t.Fatalf("disabled client tunnel should not require MTU: %v", err)
	}
}

func TestLoadValidatesDecodedConfiguration(t *testing.T) {
	dir := t.TempDir()
	clientPath := filepath.Join(dir, "client.yaml")
	if err := os.WriteFile(clientPath, []byte("server: not-a-url\nname: test\nstate_dir: ./state\nlocal_address: 127.0.0.1:9070\nkeepalive: 25s\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadClient(clientPath); err == nil {
		t.Fatal("expected decoded client configuration to be rejected")
	}

	serverPath := filepath.Join(dir, "server.yaml")
	if err := os.WriteFile(serverPath, []byte("listen_address: invalid\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadServer(serverPath); err == nil {
		t.Fatal("expected decoded server configuration to be rejected")
	}
}
