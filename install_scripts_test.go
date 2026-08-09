package tyxnet_test

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestLinuxInstallScripts(t *testing.T) {
	tests := []struct {
		path     string
		required []string
	}{
		{"install-server.sh", []string{"tyxnet-server-linux-$arch", "tyxnetctl-linux-$arch", "sha256sum -c -", "systemctl enable", "/etc/tyxnet/server.yaml"}},
		{"install-client.sh", []string{"tyxnet-client-linux-$arch", "sha256sum -c -", "systemctl enable", "/etc/tyxnet/client.yaml", `"./$client_asset" install`}},
	}
	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			contents, err := os.ReadFile(test.path)
			if err != nil {
				t.Fatal(err)
			}
			for _, required := range test.required {
				if !strings.Contains(string(contents), required) {
					t.Errorf("%s does not contain %q", test.path, required)
				}
			}
			if output, err := exec.Command("sh", "-n", test.path).CombinedOutput(); err != nil {
				t.Fatalf("shell syntax: %v: %s", err, output)
			}
		})
	}
	release, err := os.ReadFile("scripts/release.sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"install-server.sh", "install-client.sh"} {
		if !strings.Contains(string(release), name) {
			t.Errorf("release does not include %s", name)
		}
	}
	makefile, err := os.ReadFile("Makefile")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(makefile), "release-full: release package-macos package-windows\n\tsh scripts/checksums.sh") {
		t.Error("release-full does not regenerate checksums after installer packages")
	}
}
