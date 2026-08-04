package config

import "testing"

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
func TestServerRejectsPublicPlainHTTP(t *testing.T) {
	c := DefaultServer()
	c.ListenAddress = "0.0.0.0"
	if err := c.Validate(); err == nil {
		t.Fatal("expected public plaintext bind to fail")
	}
	c.AllowInsecureHTTP = true
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestClientLocalAPIMustBeLoopback(t *testing.T) {
	c := DefaultClient()
	c.ServerURL = "https://example.test"
	c.Name = "node"
	c.LocalAddress = "0.0.0.0:9070"
	if err := c.Validate(); err == nil {
		t.Fatal("expected unsafe bind to fail")
	}
}
