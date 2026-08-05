//go:build linux

package application

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
)

func StartupEnabled(spec StartupSpec) (bool, error) {
	if available, _ := StartupAvailable(); !available {
		return false, nil
	}
	err := exec.Command("systemctl", "is-enabled", "--quiet", spec.ID+".service").Run()
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
	if available, reason := StartupAvailable(); !available {
		return errors.New(reason)
	}
	unitPath := "/etc/systemd/system/" + spec.ID + ".service"
	envPath := "/etc/tyxnet/" + spec.ID + "-startup.env"
	if !enabled {
		_ = exec.Command("systemctl", "disable", spec.ID+".service").Run()
		_ = os.Remove(unitPath)
		_ = os.Remove(envPath)
		_ = removeLinuxCompanion(spec)
		return exec.Command("systemctl", "daemon-reload").Run()
	}
	if err := os.MkdirAll("/etc/tyxnet", 0750); err != nil {
		return err
	}
	if err := os.WriteFile(envPath, []byte("TYXNET_TRAY_TOKEN="+spec.TrayToken+"\n"), 0600); err != nil {
		return err
	}
	unit := "[Unit]\nDescription=" + spec.DisplayName + "\nAfter=network-online.target\nWants=network-online.target\n\n[Service]\nEnvironmentFile=" + envPath + "\nWorkingDirectory=" + strconv.Quote(spec.WorkingDirectory) + "\nExecStart=" + systemdLine(spec.Executable, spec.Arguments) + "\nRestart=on-failure\nRestartSec=5\nAmbientCapabilities=CAP_NET_ADMIN\nCapabilityBoundingSet=CAP_NET_ADMIN\nNoNewPrivileges=true\n\n[Install]\nWantedBy=multi-user.target\n"
	if err := os.WriteFile(unitPath, []byte(unit), 0644); err != nil {
		return fmt.Errorf("write systemd unit (root required): %w", err)
	}
	if err := writeLinuxCompanion(spec); err != nil {
		return err
	}
	if out, err := exec.Command("systemctl", "daemon-reload").CombinedOutput(); err != nil {
		return fmt.Errorf("systemd reload: %w: %s", err, strings.TrimSpace(string(out)))
	}
	out, err := exec.Command("systemctl", "enable", spec.ID+".service").CombinedOutput()
	if err != nil {
		return fmt.Errorf("enable systemd service: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func StartupAvailable() (bool, string) {
	if os.Getenv("TYXNET_CONTAINER") != "" {
		return false, "startup is managed by the container runtime"
	}
	for _, marker := range []string{"/.dockerenv", "/run/.containerenv"} {
		if _, err := os.Stat(marker); err == nil {
			return false, "startup is managed by the container runtime"
		}
	}
	if _, err := exec.LookPath("systemctl"); err != nil {
		return false, "systemd is not available on this host"
	}
	initName, err := os.ReadFile("/proc/1/comm")
	if err != nil || strings.TrimSpace(string(initName)) != "systemd" {
		return false, "systemd is not the active service manager"
	}
	return true, ""
}

func systemdLine(executable string, args []string) string {
	parts := []string{strconv.Quote(strings.ReplaceAll(executable, "%", "%%"))}
	for _, arg := range args {
		parts = append(parts, strconv.Quote(strings.ReplaceAll(arg, "%", "%%")))
	}
	return strings.Join(parts, " ")
}

func desktopUser() (*user.User, error) {
	name := os.Getenv("SUDO_USER")
	if name != "" && name != "root" {
		return user.Lookup(name)
	}
	return user.Current()
}

func linuxCompanionPath(spec StartupSpec) (string, *user.User, error) {
	u, err := desktopUser()
	if err != nil {
		return "", nil, err
	}
	return filepath.Join(u.HomeDir, ".config", "autostart", spec.ID+"-tray.desktop"), u, nil
}

func writeLinuxCompanion(spec StartupSpec) error {
	if spec.Companion == "" {
		return nil
	}
	path, u, err := linuxCompanionPath(spec)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	args := append([]string{"TYXNET_TRAY_TOKEN=" + spec.TrayToken, spec.Companion}, spec.CompanionArgs...)
	data := "[Desktop Entry]\nType=Application\nName=" + spec.DisplayName + " Tray\nExec=" + systemdLine("/usr/bin/env", args) + "\nX-GNOME-Autostart-enabled=true\n"
	if err := os.WriteFile(path, []byte(data), 0644); err != nil {
		return err
	}
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)
	return os.Chown(path, uid, gid)
}

func removeLinuxCompanion(spec StartupSpec) error {
	path, _, err := linuxCompanionPath(spec)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
