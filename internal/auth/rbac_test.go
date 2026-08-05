package auth

import "testing"

func TestRBAC(t *testing.T) {
	if !Allowed(Admin, "server.configure") || !Allowed(Operator, "network.flow.view") || !Allowed(Viewer, "network.flow.view") || Allowed(Member, "network.flow.view") || Allowed(Viewer, "device.shutdown") {
		t.Fatal("incorrect permissions")
	}
}
