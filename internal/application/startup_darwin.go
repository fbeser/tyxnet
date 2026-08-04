//go:build darwin

package application

import (
	"errors"
	"fmt"
	"html"
	"os"
	"os/exec"
	"strings"
)

func StartupEnabled(spec StartupSpec) (bool, error) {
	_, err := os.Stat(daemonPath(spec))
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
}

func SetStartup(spec StartupSpec, enabled bool) error {
	daemon := daemonPath(spec)
	agent := agentPath(spec)
	if !enabled {
		_ = exec.Command("launchctl", "disable", "system/"+launchLabel(spec)).Run()
		_ = os.Remove(daemon)
		_ = os.Remove(agent)
		return nil
	}
	if os.Geteuid() != 0 {
		return errors.New("root privileges are required to configure macOS startup")
	}
	if err := os.WriteFile(daemon, []byte(plist(launchLabel(spec), spec.Executable, spec.Arguments, spec.WorkingDirectory, spec.TrayToken, true)), 0600); err != nil {
		return err
	}
	if spec.Companion != "" {
		if err := os.WriteFile(agent, []byte(plist(launchLabel(spec)+".tray", spec.Companion, spec.CompanionArgs, spec.WorkingDirectory, spec.TrayToken, false)), 0600); err != nil {
			return err
		}
	}
	out, err := exec.Command("launchctl", "enable", "system/"+launchLabel(spec)).CombinedOutput()
	if err != nil {
		return fmt.Errorf("enable LaunchDaemon: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func launchLabel(spec StartupSpec) string {
	return "com.tyxnet." + strings.TrimPrefix(spec.ID, "tyxnet-")
}
func daemonPath(spec StartupSpec) string {
	return "/Library/LaunchDaemons/" + launchLabel(spec) + ".plist"
}
func agentPath(spec StartupSpec) string {
	return "/Library/LaunchAgents/" + launchLabel(spec) + ".tray.plist"
}

func plist(label, executable string, args []string, workingDirectory, trayToken string, keepAlive bool) string {
	values := append([]string{executable}, args...)
	var arguments strings.Builder
	for _, value := range values {
		arguments.WriteString("    <string>" + html.EscapeString(value) + "</string>\n")
	}
	processType := "Interactive"
	if keepAlive {
		processType = "Background"
	}
	keepAliveXML := "<false/>"
	if keepAlive {
		keepAliveXML = "<dict><key>SuccessfulExit</key><false/></dict>"
	}
	return "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<!DOCTYPE plist PUBLIC \"-//Apple//DTD PLIST 1.0//EN\" \"http://www.apple.com/DTDs/PropertyList-1.0.dtd\">\n<plist version=\"1.0\"><dict>\n  <key>Label</key><string>" + html.EscapeString(label) + "</string>\n  <key>ProgramArguments</key><array>\n" + arguments.String() + "  </array>\n  <key>WorkingDirectory</key><string>" + html.EscapeString(workingDirectory) + "</string>\n  <key>EnvironmentVariables</key><dict><key>TYXNET_TRAY_TOKEN</key><string>" + html.EscapeString(trayToken) + "</string></dict>\n  <key>RunAtLoad</key><true/>\n  <key>KeepAlive</key>" + keepAliveXML + "\n  <key>ProcessType</key><string>" + processType + "</string>\n</dict></plist>\n"
}
