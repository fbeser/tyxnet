package commands

import (
	"context"
	"runtime"
	"slices"
	"testing"
)

func TestRejectsArbitraryCommand(t *testing.T) {
	if err := ExecuteSystem(context.Background(), "echo unsafe"); err == nil {
		t.Fatal("arbitrary command accepted")
	}
}

func TestSystemCommandsUseFixedArguments(t *testing.T) {
	tests := map[string]struct {
		name string
		args []string
	}{
		"linux/system.restart":    {name: "systemctl", args: []string{"reboot"}},
		"linux/system.shutdown":   {name: "systemctl", args: []string{"poweroff"}},
		"windows/system.restart":  {name: "shutdown.exe", args: []string{"/r", "/t", "5", "/d", "p:0:0", "/c", "TyxNet administrator requested a restart"}},
		"windows/system.shutdown": {name: "shutdown.exe", args: []string{"/s", "/t", "5", "/d", "p:0:0", "/c", "TyxNet administrator requested a shutdown"}},
		"darwin/system.restart":   {name: "/sbin/shutdown", args: []string{"-r", "now"}},
		"darwin/system.shutdown":  {name: "/sbin/shutdown", args: []string{"-h", "now"}},
	}
	for _, commandType := range []string{"system.restart", "system.shutdown"} {
		want := tests[runtime.GOOS+"/"+commandType]
		if want.name == "" {
			t.Skip("system command is unsupported on this test platform")
		}
		name, args, err := systemCommand(commandType)
		if err != nil || name != want.name || !slices.Equal(args, want.args) {
			t.Fatalf("%s command = %s %v %v", commandType, name, args, err)
		}
	}
}
