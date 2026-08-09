package storage

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
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
	var migrationCount int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations WHERE version=2").Scan(&migrationCount); err != nil || migrationCount != 1 {
		t.Fatalf("flow history migration missing: count=%d err=%v", migrationCount, err)
	}
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

func TestFlowHistoryMigrationUpgradesExistingDatabase(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "tyxnet.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	initial, err := migrations.ReadFile("migrations/001_initial.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, string(initial)); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, "INSERT INTO schema_migrations(version,applied_at) VALUES(1,?)", time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, "INSERT INTO server_settings(key,value,updated_at) VALUES('ping_interval','25s',?)", time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	var migrated int
	if err = s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations WHERE version=2").Scan(&migrated); err != nil || migrated != 1 {
		t.Fatalf("migration 2 was not applied: %d %v", migrated, err)
	}
	if value, err := s.Setting(ctx, "ping_interval"); err != nil || value != "25s" {
		t.Fatalf("existing setting was not preserved: %q %v", value, err)
	}
}

func TestFlowHistoryFilteringTrimmingAndDeletion(t *testing.T) {
	ctx := context.Background()
	s := setup(t)
	base := time.Date(2026, time.August, 9, 9, 30, 0, 0, time.UTC)
	sourcePort, httpsPort, sshPort := uint16(52000), uint16(443), uint16(22)
	records := []FlowHistoryRecord{
		{RecordedAt: base, Source: "10.90.0.2", Destination: "10.90.0.3", Protocol: "udp", ProtocolNumber: 17, SourcePort: &sourcePort, DestinationPort: &httpsPort, Bytes: 10, Packets: 1},
		{RecordedAt: base.Add(time.Second), Source: "10.90.0.2", Destination: "10.90.0.3", Protocol: "tcp", ProtocolNumber: 6, SourcePort: &sourcePort, DestinationPort: &sshPort, Bytes: 30, Packets: 3},
		{RecordedAt: base.Add(2 * time.Second), Source: "10.90.0.3", Destination: "10.90.0.2", Protocol: "tcp", ProtocolNumber: 6, SourcePort: &httpsPort, DestinationPort: &sourcePort, Bytes: 20, Packets: 2},
	}
	if err := s.InsertFlowHistory(ctx, records, 240); err != nil {
		t.Fatal(err)
	}
	page, err := s.ListFlowHistory(ctx, FlowHistoryQuery{Protocol: "tcp", Sort: "oldest", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 2 || len(page.Records) != 2 || page.Records[0].DestinationPort == nil || *page.Records[0].DestinationPort != 22 || page.StoredBytes > 240 {
		t.Fatalf("unexpected trimmed flow history: %+v", page)
	}
	from := base.Add(2 * time.Second)
	page, err = s.ListFlowHistory(ctx, FlowHistoryQuery{Search: "laptop", EndpointIPs: []string{"10.90.0.2"}, From: &from, Sort: "bytes", Limit: 10})
	if err != nil || page.Total != 1 || len(page.Records) != 1 || page.Records[0].Bytes != 20 {
		t.Fatalf("unexpected filtered flow history: %+v %v", page, err)
	}
	deleted, err := s.DeleteFlowHistory(ctx)
	if err != nil || deleted != 2 {
		t.Fatalf("delete flow history: count=%d err=%v", deleted, err)
	}
	page, err = s.ListFlowHistory(ctx, FlowHistoryQuery{})
	if err != nil || page.Total != 0 || page.StoredBytes != 0 || page.Records == nil {
		t.Fatalf("flow history was not cleared: %+v %v", page, err)
	}
}

func TestSetSettingsPersistsAtomically(t *testing.T) {
	ctx := context.Background()
	s := setup(t)
	if err := s.SetSettings(ctx, map[string]string{"flow_history_enabled": "true", "flow_history_limit_mb": "100"}); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{"flow_history_enabled": "true", "flow_history_limit_mb": "100"} {
		if got, err := s.Setting(ctx, key); err != nil || got != want {
			t.Fatalf("setting %s=%q, want %q: %v", key, got, want, err)
		}
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
func TestCommandDeliveryLifecycle(t *testing.T) {
	ctx := context.Background()
	s := setup(t)
	admin, _ := s.CreateAdmin(ctx, "admin", "hash")
	_, enrollment, _ := s.CreateEnrollmentToken(ctx, admin.ID, time.Hour, 1)
	device, err := s.JoinDevice(ctx, enrollment, "device", "linux", "arm64", "dev", "10.90.0.0/24", []byte("key"))
	if err != nil {
		t.Fatal(err)
	}
	command, err := s.CreateCommand(ctx, admin.ID, device.ID, "system.restart", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := s.PendingCommandsForDevice(ctx, device.ID, 10)
	if err != nil || len(pending) != 1 || pending[0].ID != command.ID {
		t.Fatalf("pending commands: %+v %v", pending, err)
	}
	if err = s.MarkCommandDelivered(ctx, command.ID, device.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.UpdateCommandResult(ctx, command.ID, device.ID, "accepted", "", ""); err != nil {
		t.Fatal(err)
	}
	if pending, err = s.PendingCommandsForDevice(ctx, device.ID, 10); err != nil || len(pending) != 0 {
		t.Fatalf("accepted command remained pending: %+v %v", pending, err)
	}
	if err = s.UpdateCommandResult(ctx, command.ID, device.ID, "succeeded", "system action scheduled", ""); err != nil {
		t.Fatal(err)
	}
	if err = s.UpdateCommandResult(ctx, command.ID, device.ID, "succeeded", "system action scheduled", ""); err != nil {
		t.Fatalf("idempotent result failed: %v", err)
	}
	if err = s.UpdateCommandResult(ctx, command.ID, device.ID, "failed", "", "late failure"); err == nil {
		t.Fatal("invalid terminal status transition was accepted")
	}
	commands, err := s.ListCommands(ctx, 10)
	if err != nil || len(commands) != 1 || commands[0].Status != "succeeded" || commands[0].Result != "system action scheduled" {
		t.Fatalf("completed command: %+v %v", commands, err)
	}
}

func TestExpiredAndRevokedDeviceCommandsAreRejected(t *testing.T) {
	ctx := context.Background()
	s := setup(t)
	admin, _ := s.CreateAdmin(ctx, "admin", "hash")
	_, enrollment, _ := s.CreateEnrollmentToken(ctx, admin.ID, time.Hour, 1)
	device, _ := s.JoinDevice(ctx, enrollment, "device", "linux", "arm64", "dev", "10.90.0.0/24", []byte("key"))
	command, err := s.CreateCommand(ctx, admin.ID, device.ID, "system.shutdown", time.Nanosecond)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Millisecond)
	if pending, pendingErr := s.PendingCommandsForDevice(ctx, device.ID, 10); pendingErr != nil || len(pending) != 0 {
		t.Fatalf("expired command was delivered: %+v %v", pending, pendingErr)
	}
	commands, _ := s.ListCommands(ctx, 10)
	if len(commands) != 1 || commands[0].ID != command.ID || commands[0].Status != "expired" {
		t.Fatalf("expired command status: %+v", commands)
	}
	if err = s.RevokeDevice(ctx, device.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.CreateCommand(ctx, admin.ID, device.ID, "system.restart", time.Minute); err == nil {
		t.Fatal("command was queued for a revoked device")
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

func TestUpdateUserPasswordRevokesSessions(t *testing.T) {
	ctx := context.Background()
	s := setup(t)
	user, err := s.CreateAdmin(ctx, "admin", "old-hash")
	if err != nil {
		t.Fatal(err)
	}
	session, err := s.CreateSession(ctx, user.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.UpdateUserPassword(ctx, user.ID, "new-hash"); err != nil {
		t.Fatal(err)
	}
	_, passwordHash, err := s.Authenticate(ctx, user.Username)
	if err != nil || passwordHash != "new-hash" {
		t.Fatalf("password hash was not updated: %q %v", passwordHash, err)
	}
	if _, err = s.SessionUser(ctx, session); err == nil {
		t.Fatal("existing session remained valid after password update")
	}
	if err = s.UpdateUserPassword(ctx, "missing-user", "hash"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing user error = %v, want sql.ErrNoRows", err)
	}
}
