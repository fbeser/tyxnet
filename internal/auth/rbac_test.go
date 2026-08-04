package auth

import "testing"

func TestRBAC(t *testing.T) {
	if !Allowed(Admin, "server.configure") || Allowed(Viewer, "device.shutdown") {
		t.Fatal("incorrect permissions")
	}
}
