package docker_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type composeFile struct {
	Version  string                    `yaml:"version"`
	Services map[string]composeService `yaml:"services"`
	Volumes  map[string]any            `yaml:"volumes"`
}

type composeService struct {
	Image       string            `yaml:"image"`
	Command     []string          `yaml:"command"`
	Environment map[string]string `yaml:"environment"`
	Ports       []string          `yaml:"ports"`
}

func TestCanonicalComposeSupportsLANAndHTTPS(t *testing.T) {
	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate compose test")
	}
	composePath := filepath.Join(filepath.Dir(testFile), "..", "..", "docker-compose.yml")
	data, err := os.ReadFile(composePath)
	if err != nil {
		t.Fatal(err)
	}

	var config composeFile
	if err := yaml.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	if config.Version != "3.3" {
		t.Fatalf("compose version = %q", config.Version)
	}

	server, ok := config.Services["tyxnet-server"]
	if !ok {
		t.Fatal("tyxnet-server service is missing")
	}
	if !strings.Contains(server.Image, "${TYXNET_VERSION:-latest}") {
		t.Fatalf("server image is not version-configurable: %q", server.Image)
	}
	assertContains(t, server.Ports, "${TYXNET_LAN_PORT:-8443}:8443/tcp")
	assertContains(t, server.Ports, "${TYXNET_TUNNEL_PORT:-51830}:51830/udp")

	caddy, ok := config.Services["caddy"]
	if !ok {
		t.Fatal("caddy service is missing")
	}
	command := strings.Join(caddy.Command, "\n")
	for _, required := range []string{"TYXNET_DOMAIN", "TYXNET_PUBLIC_IP", "default_sni", "profile shortlived", "reverse_proxy tyxnet-server:8443"} {
		if !strings.Contains(command, required) {
			t.Fatalf("caddy command does not contain %q", required)
		}
	}
	assertContains(t, caddy.Ports, "${TYXNET_HTTP_CHALLENGE_PORT:-18080}:80/tcp")
	assertContains(t, caddy.Ports, "${TYXNET_HTTPS_PORT:-18443}:443/tcp")
	assertContains(t, caddy.Ports, "${TYXNET_HTTPS_PORT:-18443}:443/udp")
	for _, volume := range []string{"tyxnet-data", "caddy-data", "caddy-config"} {
		if _, ok := config.Volumes[volume]; !ok {
			t.Fatalf("persistent volume %q is missing", volume)
		}
	}
}

func assertContains(t *testing.T, values []string, expected string) {
	t.Helper()
	for _, value := range values {
		if value == expected {
			return
		}
	}
	t.Fatalf("%q is missing from %v", expected, values)
}
