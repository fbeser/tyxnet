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
	Volumes  map[string]composeVolume  `yaml:"volumes"`
	Networks map[string]composeNetwork `yaml:"networks"`
}

type composeService struct {
	Image       string            `yaml:"image"`
	Command     []string          `yaml:"command"`
	Entrypoint  []string          `yaml:"entrypoint"`
	Environment map[string]string `yaml:"environment"`
	Ports       []string          `yaml:"ports"`
	Networks    map[string]struct {
		Aliases []string `yaml:"aliases"`
	} `yaml:"networks"`
}

type composeVolume struct {
	Name string `yaml:"name"`
}

type composeNetwork struct {
	Name   string `yaml:"name"`
	Driver string `yaml:"driver"`
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
	if len(server.Environment) != 0 {
		t.Fatalf("server must not inherit host environment: %v", server.Environment)
	}
	assertContains(t, server.Ports, "${TYXNET_LAN_PORT:-8443}:8443/tcp")
	assertContains(t, server.Ports, "${TYXNET_TUNNEL_PORT:-51830}:51830/udp")
	serverNetwork, ok := server.Networks["tyxnet"]
	if !ok {
		t.Fatal("server is not attached to the tyxnet network")
	}
	assertContains(t, serverNetwork.Aliases, "tyxnet-server")

	caddy, ok := config.Services["caddy"]
	if !ok {
		t.Fatal("caddy service is missing")
	}
	if caddy.Image != server.Image {
		t.Fatalf("caddy image %q does not match server image %q", caddy.Image, server.Image)
	}
	if len(caddy.Entrypoint) != 1 || caddy.Entrypoint[0] != "/usr/local/bin/tyxnet-caddy-entrypoint" {
		t.Fatalf("unexpected caddy entrypoint: %v", caddy.Entrypoint)
	}
	if len(caddy.Command) != 0 {
		t.Fatalf("caddy command must not contain inline shell: %v", caddy.Command)
	}
	if len(caddy.Environment) != 2 {
		t.Fatalf("unexpected caddy environment: %v", caddy.Environment)
	}
	for _, variable := range []string{"TYXNET_DOMAIN", "TYXNET_PUBLIC_IP"} {
		if _, ok := caddy.Environment[variable]; !ok {
			t.Fatalf("caddy environment does not contain %q", variable)
		}
	}
	assertContains(t, caddy.Ports, "${TYXNET_HTTP_CHALLENGE_PORT:-18080}:80/tcp")
	assertContains(t, caddy.Ports, "${TYXNET_HTTPS_PORT:-18443}:443/tcp")
	assertContains(t, caddy.Ports, "${TYXNET_HTTPS_PORT:-18443}:443/udp")
	if _, ok := caddy.Networks["tyxnet"]; !ok {
		t.Fatal("caddy is not attached to the tyxnet network")
	}

	for volume, expectedName := range map[string]string{
		"tyxnet-data":  "${TYXNET_DATA_VOLUME:-tyxnet_tyxnet-data}",
		"caddy-data":   "${TYXNET_CADDY_DATA_VOLUME:-tyxnet_caddy-data}",
		"caddy-config": "${TYXNET_CADDY_CONFIG_VOLUME:-tyxnet_caddy-config}",
	} {
		if config.Volumes[volume].Name != expectedName {
			t.Fatalf("volume %q name = %q", volume, config.Volumes[volume].Name)
		}
	}
	if network := config.Networks["tyxnet"]; network.Name != "" || network.Driver != "bridge" {
		t.Fatalf("unexpected tyxnet network: %+v", network)
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
