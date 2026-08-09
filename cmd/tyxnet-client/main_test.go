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

func TestInstallAllowsWebEnrollmentOrCompleteCredentials(t *testing.T) {
	c, configured, err := installConfiguration("", "", "")
	if err != nil || configured || c.StateDir != "/var/lib/tyxnet/client" {
		t.Fatalf("unconfigured install: %+v configured=%t err=%v", c, configured, err)
	}
	_, configured, err = installConfiguration("https://vpn.example.com", "TYX-test", "raspberry-pi")
	if err != nil || !configured {
		t.Fatalf("configured install: configured=%t err=%v", configured, err)
	}
	if _, _, err = installConfiguration("https://vpn.example.com", "", "raspberry-pi"); err == nil {
		t.Fatal("partial enrollment credentials were accepted")
	}
}
