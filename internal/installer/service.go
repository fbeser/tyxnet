package installer

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

type Spec struct {
	Name, Binary, Config string
	ConfigData           []byte
}

func Install(spec Spec) error {
	if runtime.GOOS != "linux" {
		return errors.New("native service install is not implemented on this platform")
	}
	dst := "/usr/local/bin/" + spec.Binary
	if err := copyFile(os.Args[0], dst, 0755); err != nil {
		return fmt.Errorf("copy binary (Linux/root required): %w", err)
	}
	if err := os.MkdirAll("/etc/tyxnet", 0750); err != nil {
		return err
	}
	if err := os.MkdirAll("/var/lib/tyxnet", 0750); err != nil {
		return err
	}
	if err := os.MkdirAll("/var/log/tyxnet", 0750); err != nil {
		return err
	}
	if err := os.WriteFile(spec.Config, spec.ConfigData, 0600); err != nil {
		return err
	}
	unit := fmt.Sprintf("[Unit]\nDescription=TyxNet %s\nAfter=network-online.target\nWants=network-online.target\n\n[Service]\nExecStart=%s run --config %s\nRestart=on-failure\nRestartSec=5\nNoNewPrivileges=true\nPrivateTmp=true\nProtectSystem=strict\nReadWritePaths=/var/lib/tyxnet /var/log/tyxnet /etc/tyxnet /etc/systemd/system\n\n[Install]\nWantedBy=multi-user.target\n", spec.Name, dst, spec.Config)
	if err := os.WriteFile("/etc/systemd/system/"+spec.Name+".service", []byte(unit), 0644); err != nil {
		return err
	}
	return fixed("systemctl", "daemon-reload").then("systemctl", "enable", "--now", spec.Name+".service")
}
func Uninstall(name, binary, config string) error {
	if runtime.GOOS != "linux" {
		return errors.New("native service uninstall is not implemented on this platform")
	}
	_ = exec.Command("systemctl", "disable", "--now", name+".service").Run()
	_ = os.Remove("/etc/systemd/system/" + name + ".service")
	_ = os.Remove("/usr/local/bin/" + binary)
	_ = os.Remove(config)
	return exec.Command("systemctl", "daemon-reload").Run()
}

type result struct{ err error }

func fixed(name string, args ...string) result { return result{exec.Command(name, args...).Run()} }
func (r result) then(name string, args ...string) error {
	if r.err != nil {
		return r.err
	}
	return exec.Command(name, args...).Run()
}
func copyFile(src, dst string, mode os.FileMode) error {
	b, err := os.ReadFile(filepath.Clean(src))
	if err != nil {
		return err
	}
	return os.WriteFile(dst, b, mode)
}
func ServiceAction(name, action string) error {
	if runtime.GOOS != "linux" {
		return errors.New("native service management is not implemented on this platform")
	}
	allowed := map[string][]string{"start": {"start"}, "stop": {"stop"}, "restart": {"restart"}, "status": {"status", "--no-pager"}, "logs": {"-u", name + ".service", "-f"}}
	args, ok := allowed[action]
	if !ok {
		return errors.New("unsupported service action")
	}
	if action == "logs" {
		return exec.Command("journalctl", args...).Run()
	}
	return exec.Command("systemctl", append(args, name+".service")...).Run()
}
