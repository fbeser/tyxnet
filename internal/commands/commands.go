package commands

import (
	"context"
	"errors"
	"os/exec"
	"runtime"
)

var Allowed = map[string]bool{"system.restart": true, "system.shutdown": true, "client.reconnect": true, "client.status": true, "client.update-check": true, "logs.collect": true}

// ExecuteSystem performs only fixed, audited operating-system actions. No input
// is ever interpreted as a shell command.
func ExecuteSystem(ctx context.Context, typ string) error {
	if !Allowed[typ] {
		return errors.New("command is not allowlisted")
	}
	var name string
	var args []string
	switch typ {
	case "system.restart":
		if runtime.GOOS == "windows" {
			name = "shutdown"
			args = []string{"/r", "/t", "0"}
		} else {
			name = "systemctl"
			args = []string{"reboot"}
		}
	case "system.shutdown":
		if runtime.GOOS == "windows" {
			name = "shutdown"
			args = []string{"/s", "/t", "0"}
		} else {
			name = "systemctl"
			args = []string{"poweroff"}
		}
	default:
		return errors.New("command is handled internally and has no system action")
	}
	return exec.CommandContext(ctx, name, args...).Run()
}
