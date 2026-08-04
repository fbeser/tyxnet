//go:build windows

package application

import (
	"strings"
	"testing"
)

func TestPowerShellStartupQuoting(t *testing.T) {
	line := psArray([]string{"run", "C:\\TyxNet's Files\\client.yaml"})
	if !strings.Contains(line, "TyxNet''s Files") {
		t.Fatalf("PowerShell literal was not escaped: %s", line)
	}
	if taskName(StartupSpec{DisplayName: "TyxNet Client"}) != "TyxNet Client" {
		t.Fatal("unexpected scheduled task name")
	}
	script := windowsLauncher(StartupSpec{Executable: `C:\TyxNet\client.exe`, Companion: `C:\TyxNet\tray.exe`, WorkingDirectory: `C:\TyxNet`, TrayToken: "secret"})
	for _, expected := range []string{"TYXNET_TRAY_TOKEN", "WindowStyle Hidden", "WaitForExit"} {
		if !strings.Contains(script, expected) {
			t.Fatalf("startup launcher is missing %s", expected)
		}
	}
}
