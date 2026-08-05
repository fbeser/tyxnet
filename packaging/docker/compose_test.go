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
	Networks map[string]any            `yaml:"networks"`
}

type composeService struct {
	Image       string            `yaml:"image"`
	Environment map[string]string `yaml:"environment"`
	CapAdd      []string          `yaml:"cap_add"`
	Devices     []string          `yaml:"devices"`
	Ports       []string          `yaml:"ports"`
	Volumes     []string          `yaml:"volumes"`
}

type composeVolume struct {
	Name string `yaml:"name"`
}

func TestCanonicalComposeUsesOneContainer(t *testing.T) {
	composePath := repositoryFile(t, "docker-compose.yml")
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
	if len(config.Services) != 1 {
		t.Fatalf("compose services = %v", config.Services)
	}
	service, ok := config.Services["tyxnet-server"]
	if !ok {
		t.Fatal("tyxnet-server service is missing")
	}
	if !strings.Contains(service.Image, "${TYXNET_VERSION:-latest}") {
		t.Fatalf("server image is not version-configurable: %q", service.Image)
	}
	if len(service.Environment) != 2 {
		t.Fatalf("unexpected container environment: %v", service.Environment)
	}
	for _, variable := range []string{"TYXNET_DOMAIN", "TYXNET_PUBLIC_IP"} {
		if _, ok := service.Environment[variable]; !ok {
			t.Fatalf("container environment does not contain %q", variable)
		}
	}
	assertContains(t, service.CapAdd, "NET_ADMIN")
	assertContains(t, service.Devices, "/dev/net/tun:/dev/net/tun")
	for _, port := range []string{
		"${TYXNET_LAN_PORT:-8443}:8443/tcp",
		"${TYXNET_TUNNEL_PORT:-51830}:51830/udp",
		"${TYXNET_HTTP_CHALLENGE_PORT:-18080}:80/tcp",
		"${TYXNET_HTTPS_PORT:-18443}:443/tcp",
		"${TYXNET_HTTPS_PORT:-18443}:443/udp",
	} {
		assertContains(t, service.Ports, port)
	}
	for _, volume := range []string{
		"tyxnet-data:/var/lib/tyxnet",
		"caddy-data:/data",
		"caddy-config:/config",
	} {
		assertContains(t, service.Volumes, volume)
	}
	if len(config.Networks) != 0 {
		t.Fatalf("single-container compose should not define networks: %v", config.Networks)
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
}

func TestDockerImageUsesSupervisorEntrypoint(t *testing.T) {
	data, err := os.ReadFile(repositoryFile(t, "packaging", "docker", "Dockerfile"))
	if err != nil {
		t.Fatal(err)
	}
	dockerfile := string(data)
	for _, expected := range []string{
		"COPY packaging/docker/entrypoint.sh /usr/local/bin/tyxnet-entrypoint",
		"STOPSIGNAL SIGTERM",
		"ENTRYPOINT [\"/usr/local/bin/tyxnet-entrypoint\"]",
	} {
		if !strings.Contains(dockerfile, expected) {
			t.Fatalf("Dockerfile does not contain %q", expected)
		}
	}
}

func repositoryFile(t *testing.T, path ...string) string {
	t.Helper()
	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate repository")
	}
	parts := append([]string{filepath.Dir(testFile), "..", ".."}, path...)
	return filepath.Join(parts...)
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
