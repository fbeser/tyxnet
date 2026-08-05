package docker_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestEntrypointStartsServerBeforeCaddyAndStopsBoth(t *testing.T) {
	run := startEntrypoint(t, map[string]string{"TYXNET_DOMAIN": "localhost"})
	waitForEvents(t, run.eventsPath, "server-start", "caddy-start")
	events := readFile(t, run.eventsPath)
	if strings.Index(events, "server-start") > strings.Index(events, "caddy-start") {
		t.Fatalf("Caddy started before TyxNet:\n%s", events)
	}
	config := readFile(t, run.configPath)
	if !strings.Contains(config, "reverse_proxy 127.0.0.1:8443") {
		t.Fatalf("Caddy does not proxy to the colocated server:\n%s", config)
	}
	if err := run.command.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	if err := waitCommand(run.command, run.done, 4*time.Second); err != nil {
		t.Fatalf("entrypoint did not stop cleanly: %v", err)
	}
	waitForEvents(t, run.eventsPath, "server-term", "caddy-term")
}

func TestEntrypointBuildsPublicIPAndLocalOnlyConfigs(t *testing.T) {
	for name, environment := range map[string]map[string]string{
		"public IP":  {"TYXNET_PUBLIC_IP": "203.0.113.10"},
		"local only": {},
	} {
		t.Run(name, func(t *testing.T) {
			run := startEntrypoint(t, environment)
			waitForEvents(t, run.eventsPath, "caddy-start")
			config := readFile(t, run.configPath)
			expected := "profile shortlived"
			if name == "local only" {
				expected = "TyxNet HTTPS is disabled"
			}
			if !strings.Contains(config, expected) {
				t.Fatalf("generated config does not contain %q:\n%s", expected, config)
			}
			if err := run.command.Process.Signal(syscall.SIGINT); err != nil {
				t.Fatal(err)
			}
			if err := waitCommand(run.command, run.done, 4*time.Second); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestEntrypointFailsWhenEitherProcessExits(t *testing.T) {
	for name, environment := range map[string]map[string]string{
		"server": {"FAKE_SERVER_EXIT": "17"},
		"caddy":  {"FAKE_CADDY_EXIT": "23"},
	} {
		t.Run(name, func(t *testing.T) {
			run := startEntrypoint(t, environment)
			err := waitCommand(run.command, run.done, 5*time.Second)
			if err == nil {
				t.Fatal("unexpected process exit returned success")
			}
			if name == "server" {
				waitForEvents(t, run.eventsPath, "caddy-term")
			} else {
				waitForEvents(t, run.eventsPath, "server-term")
			}
		})
	}
}

func TestEntrypointRejectsAmbiguousOrUnsafeAddress(t *testing.T) {
	for name, environment := range map[string]map[string]string{
		"ambiguous": {
			"TYXNET_DOMAIN":    "vpn.example.com",
			"TYXNET_PUBLIC_IP": "203.0.113.10",
		},
		"unsafe": {"TYXNET_DOMAIN": "vpn.example.com\nimport /tmp/unsafe"},
	} {
		t.Run(name, func(t *testing.T) {
			run := startEntrypoint(t, environment)
			if err := waitCommand(run.command, run.done, 3*time.Second); err == nil {
				t.Fatal("invalid address was accepted")
			}
		})
	}
}

type entrypointRun struct {
	command    *exec.Cmd
	done       <-chan error
	eventsPath string
	configPath string
}

func startEntrypoint(t *testing.T, environment map[string]string) entrypointRun {
	t.Helper()
	tempDir := t.TempDir()
	eventsPath := filepath.Join(tempDir, "events")
	configPath := filepath.Join(tempDir, "generated-Caddyfile")
	writeExecutable(t, filepath.Join(tempDir, "tyxnet-server"), `#!/bin/sh
echo server-start >>"$EVENTS_FILE"
trap 'echo server-term >>"$EVENTS_FILE"; exit 0' TERM INT
if [ -n "${FAKE_SERVER_EXIT:-}" ]; then sleep 0.5; exit "$FAKE_SERVER_EXIT"; fi
while :; do sleep 1; done
`)
	writeExecutable(t, filepath.Join(tempDir, "caddy"), `#!/bin/sh
if [ "$1" = validate ]; then cp "$3" "$CONFIG_OUTPUT"; exit 0; fi
if [ "$1" != run ]; then exit 2; fi
echo caddy-start >>"$EVENTS_FILE"
trap 'echo caddy-term >>"$EVENTS_FILE"; exit 0' TERM INT
if [ -n "${FAKE_CADDY_EXIT:-}" ]; then sleep 0.5; exit "$FAKE_CADDY_EXIT"; fi
while :; do sleep 1; done
`)
	writeExecutable(t, filepath.Join(tempDir, "wget"), "#!/bin/sh\nexit 0\n")

	command := exec.Command("sh", repositoryFile(t, "packaging", "docker", "entrypoint.sh"))
	command.Env = append(os.Environ(),
		"PATH="+tempDir+":"+os.Getenv("PATH"),
		"EVENTS_FILE="+eventsPath,
		"CONFIG_OUTPUT="+configPath,
		"TYXNET_DOMAIN=",
		"TYXNET_PUBLIC_IP=",
		"FAKE_SERVER_EXIT=",
		"FAKE_CADDY_EXIT=",
	)
	for key, value := range environment {
		command.Env = append(command.Env, key+"="+value)
	}
	if output, err := command.StderrPipe(); err != nil {
		t.Fatal(err)
	} else {
		_ = output
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- command.Wait()
	}()
	return entrypointRun{command: command, done: done, eventsPath: eventsPath, configPath: configPath}
}

func waitCommand(command *exec.Cmd, done <-chan error, timeout time.Duration) error {
	select {
	case err := <-done:
		return err
	case <-time.After(timeout):
		_ = command.Process.Kill()
		return errors.New("timed out")
	}
}

func waitForEvents(t *testing.T, path string, events ...string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		data, _ := os.ReadFile(path)
		allFound := true
		for _, event := range events {
			if !strings.Contains(string(data), event) {
				allFound = false
				break
			}
		}
		if allFound {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("events %v not found in %q", events, readFile(t, path))
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		t.Fatal(err)
	}
	return string(data)
}

func writeExecutable(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o755); err != nil {
		t.Fatal(err)
	}
}
