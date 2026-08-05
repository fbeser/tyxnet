package commands

import (
	"context"
	"errors"
	"os/exec"
)

var Allowed = map[string]bool{"system.restart": true, "system.shutdown": true, "client.reconnect": true, "client.status": true, "client.update-check": true, "logs.collect": true}

// ExecuteSystem performs only fixed, audited operating-system actions. No input
// is ever interpreted as a shell command.
func ExecuteSystem(ctx context.Context, typ string) error {
	if !Allowed[typ] {
		return errors.New("command is not allowlisted")
	}
	name, args, err := systemCommand(typ)
	if err != nil {
		return err
	}
	return exec.CommandContext(ctx, name, args...).Run()
}
