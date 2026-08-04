package main

import "testing"

func TestRunDefaultsToLANAndSupportsLocalOnly(t *testing.T) {
	configPath := "../../configs/client.yaml"
	c, _, err := runFlags([]string{"--config", configPath})
	if err != nil {
		t.Fatal(err)
	}
	if c.LocalAddress != "0.0.0.0:9070" || !c.AllowRemoteUI {
		t.Fatalf("unexpected LAN defaults: %+v", c)
	}
	c, _, err = runFlags([]string{"--config", configPath, "--local-web"})
	if err != nil {
		t.Fatal(err)
	}
	if c.LocalAddress != "127.0.0.1:9070" || c.AllowRemoteUI {
		t.Fatalf("unexpected local-only mode: %+v", c)
	}
}
