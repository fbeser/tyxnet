package docker_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCaddyEntrypointBuildsPublicIPConfig(t *testing.T) {
	config, err := runCaddyEntrypoint(t, "", "203.0.113.10")
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"default_sni 203.0.113.10", "https://203.0.113.10", "profile shortlived", "reverse_proxy tyxnet-server:8443"} {
		if !strings.Contains(config, expected) {
			t.Fatalf("generated public-IP config does not contain %q:\n%s", expected, config)
		}
	}
}

func TestCaddyEntrypointBuildsDomainConfig(t *testing.T) {
	config, err := runCaddyEntrypoint(t, "vpn.example.com", "")
	if err != nil {
		t.Fatal(err)
	}
	if config != "https://vpn.example.com {\n\treverse_proxy tyxnet-server:8443\n}\n" {
		t.Fatalf("unexpected domain config:\n%s", config)
	}
}

func TestCaddyEntrypointBuildsDisabledHTTPSConfig(t *testing.T) {
	config, err := runCaddyEntrypoint(t, "", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"admin off", "auto_https off", "http://127.0.0.1:2019", "TyxNet HTTPS is disabled"} {
		if !strings.Contains(config, expected) {
			t.Fatalf("generated disabled-HTTPS config does not contain %q:\n%s", expected, config)
		}
	}
}

func TestCaddyEntrypointRejectsAmbiguousOrUnsafeAddress(t *testing.T) {
	if _, err := runCaddyEntrypoint(t, "vpn.example.com", "203.0.113.10"); err == nil {
		t.Fatal("domain and public IP together were accepted")
	}
	if _, err := runCaddyEntrypoint(t, "vpn.example.com\nimport /tmp/unsafe", ""); err == nil {
		t.Fatal("unsafe domain was accepted")
	}
}

func runCaddyEntrypoint(t *testing.T, domain, publicIP string) (string, error) {
	t.Helper()
	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate entrypoint test")
	}
	scriptPath := filepath.Join(filepath.Dir(testFile), "caddy-entrypoint.sh")
	tempDir := t.TempDir()
	outputPath := filepath.Join(tempDir, "generated-Caddyfile")
	fakeCaddy := filepath.Join(tempDir, "caddy")
	fake := "#!/bin/sh\nif [ \"$1\" = validate ]; then exit 0; fi\nif [ \"$1\" = run ]; then cp \"$3\" \"$OUTPUT_FILE\"; exit 0; fi\nexit 1\n"
	if err := os.WriteFile(fakeCaddy, []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}

	command := exec.Command("sh", scriptPath)
	command.Env = append(os.Environ(),
		"PATH="+tempDir+":"+os.Getenv("PATH"),
		"OUTPUT_FILE="+outputPath,
		"TYXNET_DOMAIN="+domain,
		"TYXNET_PUBLIC_IP="+publicIP,
	)
	output, err := command.CombinedOutput()
	if err != nil {
		return string(output), err
	}
	config, err := os.ReadFile(outputPath)
	return string(config), err
}
