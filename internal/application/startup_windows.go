//go:build windows

package application

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func StartupAvailable() (bool, string) { return true, "" }

func StartupEnabled(spec StartupSpec) (bool, error) {
	err := exec.Command("schtasks.exe", "/Query", "/TN", taskName(spec)).Run()
	if err == nil {
		return true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return false, nil
	}
	return false, err
}

func SetStartup(spec StartupSpec, enabled bool) error {
	dir := filepath.Join(os.Getenv("ProgramData"), "TyxNet")
	launcher := filepath.Join(dir, spec.ID+"-startup.ps1")
	if !enabled {
		_ = exec.Command("schtasks.exe", "/Delete", "/TN", taskName(spec), "/F").Run()
		if err := os.Remove(launcher); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	if err := validateSpec(spec); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	whoami, err := exec.Command("whoami.exe").Output()
	if err != nil {
		return fmt.Errorf("resolve startup task owner: %w", err)
	}
	owner := strings.TrimSpace(string(whoami))
	if out, aclErr := exec.Command("icacls.exe", dir, "/inheritance:r", "/grant:r", owner+":(OI)(CI)F", "*S-1-5-18:(OI)(CI)F", "*S-1-5-32-544:(OI)(CI)F").CombinedOutput(); aclErr != nil {
		return fmt.Errorf("protect startup launcher: %w: %s", aclErr, strings.TrimSpace(string(out)))
	}
	content := windowsLauncher(spec)
	if err := os.WriteFile(launcher, []byte(content), 0600); err != nil {
		return err
	}
	taskCommand := "powershell.exe -NoProfile -ExecutionPolicy Bypass -WindowStyle Hidden -File \"" + launcher + "\""
	out, err := exec.Command("schtasks.exe", "/Create", "/TN", taskName(spec), "/TR", taskCommand, "/SC", "ONLOGON", "/RL", "HIGHEST", "/IT", "/F").CombinedOutput()
	if err != nil {
		return fmt.Errorf("create elevated startup task: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func taskName(spec StartupSpec) string { return spec.DisplayName }

func validateSpec(spec StartupSpec) error {
	for _, value := range append([]string{spec.ID, spec.DisplayName, spec.Executable, spec.WorkingDirectory, spec.Companion, spec.TrayToken}, append(spec.Arguments, spec.CompanionArgs...)...) {
		if strings.ContainsAny(value, "\r\n\"") {
			return errors.New("startup paths and arguments cannot contain quotes or newlines")
		}
	}
	return nil
}

func psArray(values []string) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, psLiteral(value))
	}
	return "@(" + strings.Join(parts, ",") + ")"
}

func psLiteral(value string) string { return "'" + strings.ReplaceAll(value, "'", "''") + "'" }

func windowsLauncher(spec StartupSpec) string {
	content := "$ErrorActionPreference = 'Stop'\r\n"
	content += "$env:TYXNET_TRAY_TOKEN = " + psLiteral(spec.TrayToken) + "\r\n"
	content += "Set-Location -LiteralPath " + psLiteral(spec.WorkingDirectory) + "\r\n"
	if spec.Companion != "" {
		content += "Start-Process -FilePath " + psLiteral(spec.Companion) + " -ArgumentList " + psArray(spec.CompanionArgs) + " -WindowStyle Hidden\r\n"
	}
	content += "$core = Start-Process -FilePath " + psLiteral(spec.Executable) + " -ArgumentList " + psArray(spec.Arguments) + " -WindowStyle Hidden -PassThru\r\n"
	content += "$core.WaitForExit()\r\nexit $core.ExitCode\r\n"
	return content
}
