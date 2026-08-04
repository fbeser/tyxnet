package application

import "testing"

func TestTrayTokenIsPresent(t *testing.T) {
	t.Setenv("TYXNET_TRAY_TOKEN", "known-token")
	if TrayToken() != "known-token" {
		t.Fatal("environment tray token was not preserved")
	}
}
