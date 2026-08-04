//go:build linux

package application

import (
	"strings"
	"testing"
)

func TestSystemdCommandEscaping(t *testing.T) {
	line := systemdLine("/opt/Tyx Net/client", []string{"run", "100%"})
	if !strings.Contains(line, `"/opt/Tyx Net/client"`) || !strings.Contains(line, "100%%") {
		t.Fatalf("unexpected systemd command: %s", line)
	}
}
