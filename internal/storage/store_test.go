package storage

import (
	"context"
	"testing"
	"time"
)

func setup(t *testing.T) *Store {
	t.Helper()
	s, err := Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}
func TestMigrationAndTokenLimits(t *testing.T) {
	ctx := context.Background()
	s := setup(t)
	u, err := s.CreateAdmin(ctx, "admin", "hash")
	if err != nil {
		t.Fatal(err)
	}
	_, v, err := s.CreateEnrollmentToken(ctx, u.ID, time.Hour, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ConsumeToken(ctx, v); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ConsumeToken(ctx, v); err == nil {
		t.Fatal("usage limit ignored")
	}
}
func TestTokenExpiration(t *testing.T) {
	ctx := context.Background()
	s := setup(t)
	u, _ := s.CreateAdmin(ctx, "admin", "hash")
	_, v, _ := s.CreateEnrollmentToken(ctx, u.ID, time.Nanosecond, 1)
	time.Sleep(time.Millisecond)
	if _, err := s.ConsumeToken(ctx, v); err == nil {
		t.Fatal("expired token accepted")
	}
}
func TestIPAllocation(t *testing.T) {
	ctx := context.Background()
	s := setup(t)
	u, _ := s.CreateAdmin(ctx, "admin", "hash")
	_, a, _ := s.CreateEnrollmentToken(ctx, u.ID, time.Hour, 1)
	d1, err := s.JoinDevice(ctx, a, "a", "linux", "amd64", "dev", "10.90.0.0/24", []byte("key"))
	if err != nil {
		t.Fatal(err)
	}
	_, b, _ := s.CreateEnrollmentToken(ctx, u.ID, time.Hour, 1)
	d2, err := s.JoinDevice(ctx, b, "b", "linux", "amd64", "dev", "10.90.0.0/24", []byte("key2"))
	if err != nil {
		t.Fatal(err)
	}
	if d1.VirtualIP != "10.90.0.2" || d2.VirtualIP != "10.90.0.3" {
		t.Fatalf("unexpected IPs %s %s", d1.VirtualIP, d2.VirtualIP)
	}
	if err := s.RenameDevice(ctx, d1.ID, "renamed-device"); err != nil {
		t.Fatal(err)
	}
	if err := s.TouchDevice(ctx, d1.ID); err != nil {
		t.Fatal(err)
	}
	devices, err := s.ListDevices(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if devices[0].Name != "renamed-device" || devices[0].LastSeen == nil {
		t.Fatalf("device update was not persisted: %+v", devices[0])
	}
}

func TestStaticIPAssignment(t *testing.T) {
	ctx := context.Background()
	s := setup(t)
	u, _ := s.CreateAdmin(ctx, "admin", "hash")
	var devices []Device
	for _, name := range []string{"a", "b"} {
		_, token, _ := s.CreateEnrollmentToken(ctx, u.ID, time.Hour, 1)
		device, err := s.JoinDevice(ctx, token, name, "linux", "amd64", "dev", "10.90.0.0/24", []byte(name))
		if err != nil {
			t.Fatal(err)
		}
		devices = append(devices, device)
	}
	if err := s.SetDeviceVirtualIP(ctx, devices[0].ID, "10.90.0.50", "10.90.0.0/24", "10.90.0.10"); err != nil {
		t.Fatal(err)
	}
	updated, err := s.Device(ctx, devices[0].ID)
	if err != nil || updated.VirtualIP != "10.90.0.50" {
		t.Fatalf("static IP was not saved: %+v %v", updated, err)
	}
	for _, ip := range []string{"10.90.0.10", "10.91.0.2", devices[1].VirtualIP} {
		if err := s.SetDeviceVirtualIP(ctx, devices[0].ID, ip, "10.90.0.0/24", "10.90.0.10"); err == nil {
			t.Fatalf("invalid or duplicate IP %s was accepted", ip)
		}
	}
}

func TestListDevicesByUser(t *testing.T) {
	ctx := context.Background()
	s := setup(t)
	first, _ := s.CreateAdmin(ctx, "first", "hash")
	second, _ := s.CreateUser(ctx, "second", "hash", "member")
	deviceIDs := map[string]string{}
	for _, owner := range []User{first, second} {
		_, token, _ := s.CreateEnrollmentToken(ctx, owner.ID, time.Hour, 1)
		device, err := s.JoinDevice(ctx, token, owner.Username+"-pc", "windows", "amd64", "dev", "10.90.0.0/24", []byte(owner.ID))
		if err != nil {
			t.Fatal(err)
		}
		deviceIDs[owner.ID] = device.ID
		if _, err := s.CreateCommand(ctx, first.ID, device.ID, "client.status", time.Minute); err != nil {
			t.Fatal(err)
		}
	}
	devices, err := s.ListDevicesByUser(ctx, second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 1 || devices[0].UserID != second.ID {
		t.Fatalf("unexpected member devices: %+v", devices)
	}
	commands, err := s.ListCommandsByUser(ctx, second.ID, 10)
	if err != nil || len(commands) != 1 || commands[0].DeviceID != deviceIDs[second.ID] {
		t.Fatalf("unexpected member commands: %+v %v", commands, err)
	}
}
func TestCommandAllowlist(t *testing.T) {
	ctx := context.Background()
	s := setup(t)
	if _, err := s.CreateCommand(ctx, "u", "d", "rm -rf", time.Minute); err == nil {
		t.Fatal("arbitrary command accepted")
	}
}
func TestInitialAdminCanOnlyBeCreatedOnce(t *testing.T) {
	ctx := context.Background()
	s := setup(t)
	if _, err := s.CreateInitialAdmin(ctx, "first", "hash"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateInitialAdmin(ctx, "second", "hash"); err == nil {
		t.Fatal("second initial admin was accepted")
	}
}
func TestManagementListsAndUpdates(t *testing.T) {
	ctx := context.Background()
	s := setup(t)
	u, err := s.CreateInitialAdmin(ctx, "admin", "hash")
	if err != nil {
		t.Fatal(err)
	}
	viewer, err := s.CreateUser(ctx, "viewer", "hash", "viewer")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.UpdateUser(ctx, viewer.ID, true, "operator"); err != nil {
		t.Fatal(err)
	}
	users, _ := s.Users(ctx)
	if len(users) != 2 {
		t.Fatalf("users=%d", len(users))
	}
	s.Audit(ctx, u.ID, "test.action", "target", "127.0.0.1", "")
	logs, err := s.ListAudit(ctx, 10)
	if err != nil || len(logs) != 1 {
		t.Fatalf("audit=%d %v", len(logs), err)
	}
}
