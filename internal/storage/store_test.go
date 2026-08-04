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
	t.Cleanup(func() { s.Close() })
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
}
func TestCommandAllowlist(t *testing.T) {
	ctx := context.Background()
	s := setup(t)
	if _, err := s.CreateCommand(ctx, "u", "d", "rm -rf", time.Minute); err == nil {
		t.Fatal("arbitrary command accepted")
	}
}
