//go:build darwin

package application

import (
	"strings"
	"testing"
)

func TestLaunchdPlistContainsProtectedRuntimeState(t *testing.T) {
	data := plist("com.tyxnet.client", "/Applications/TyxNet/client", []string{"run"}, "/Applications/TyxNet", "secret", true)
	for _, expected := range []string{"WorkingDirectory", "TYXNET_TRAY_TOKEN", "SuccessfulExit", "Background"} {
		if !strings.Contains(data, expected) {
			t.Fatalf("plist is missing %s", expected)
		}
	}
}
