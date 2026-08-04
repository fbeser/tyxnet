package storage

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/base32"
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS

type Store struct{ db *sql.DB }
type User struct {
	ID, Username, Role string
	Disabled           bool
	CreatedAt          time.Time
}
type Device struct {
	ID, UserID, Name, VirtualIP, OS, Arch, Version string
	Revoked                                        bool
	LastSeen                                       *time.Time
	CreatedAt                                      time.Time
}
type Token struct {
	ID, UserID    string
	ExpiresAt     time.Time
	MaxUses, Uses int
	Revoked       bool
	CreatedAt     time.Time
}
type Command struct {
	ID, SenderUserID, DeviceID, Type, Status, Result, Error string
	CreatedAt, ExpiresAt                                    time.Time
}

func Open(ctx context.Context, path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if _, err = db.ExecContext(ctx, "PRAGMA foreign_keys=ON; PRAGMA busy_timeout=5000;"); err != nil {
		db.Close()
		return nil, err
	}
	if err = s.Migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}
func (s *Store) Close() error { return s.db.Close() }
func (s *Store) Migrate(ctx context.Context) error {
	entries, err := migrations.ReadDir("migrations")
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, e := range entries {
		b, err := migrations.ReadFile("migrations/" + e.Name())
		if err != nil {
			return err
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, string(b)); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %s: %w", e.Name(), err)
		}
		var v int
		fmt.Sscanf(e.Name(), "%d_", &v)
		if _, err = tx.ExecContext(ctx, "INSERT OR IGNORE INTO schema_migrations(version,applied_at) VALUES(?,?)", v, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			tx.Rollback()
			return err
		}
		if err = tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}
func id(prefix string) string {
	b := make([]byte, 10)
	_, _ = rand.Read(b)
	return prefix + "_" + strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b))
}
func TokenValue() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	raw := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b)
	return "TYX-" + raw[:4] + "-" + raw[4:8] + "-" + raw[8:], nil
}
func hash(v string) []byte { h := sha256.Sum256([]byte(v)); return h[:] }

func (s *Store) CreateAdmin(ctx context.Context, username, passwordHash string) (User, error) {
	u := User{ID: id("usr"), Username: username, Role: "admin", CreatedAt: time.Now().UTC()}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "INSERT INTO users(id,username,password_hash,created_at) VALUES(?,?,?,?)", u.ID, u.Username, passwordHash, u.CreatedAt.Format(time.RFC3339Nano)); err != nil {
		return User{}, fmt.Errorf("create user: %w", err)
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO user_roles(user_id,role_id) VALUES(?, 'role_admin')", u.ID); err != nil {
		return User{}, err
	}
	return u, tx.Commit()
}
func (s *Store) Authenticate(ctx context.Context, username string) (User, string, error) {
	var u User
	var disabled int
	var created, ph string
	err := s.db.QueryRowContext(ctx, `SELECT u.id,u.username,u.disabled,u.created_at,u.password_hash,COALESCE(r.name,'member') FROM users u LEFT JOIN user_roles ur ON ur.user_id=u.id LEFT JOIN roles r ON r.id=ur.role_id WHERE u.username=?`, username).Scan(&u.ID, &u.Username, &disabled, &created, &ph, &u.Role)
	if err != nil {
		return User{}, "", err
	}
	u.Disabled = disabled != 0
	u.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	return u, ph, nil
}
func (s *Store) CreateSession(ctx context.Context, userID string, ttl time.Duration) (string, error) {
	v, err := TokenValue()
	if err != nil {
		return "", err
	}
	_, err = s.db.ExecContext(ctx, "INSERT INTO sessions(id,user_id,token_hash,expires_at,created_at) VALUES(?,?,?,?,?)", id("ses"), userID, hash(v), time.Now().UTC().Add(ttl).Format(time.RFC3339Nano), time.Now().UTC().Format(time.RFC3339Nano))
	return v, err
}
func (s *Store) SessionUser(ctx context.Context, v string) (User, error) {
	var u User
	var disabled int
	var exp, created string
	err := s.db.QueryRowContext(ctx, `SELECT u.id,u.username,u.disabled,u.created_at,s.expires_at,COALESCE(r.name,'member') FROM sessions s JOIN users u ON u.id=s.user_id LEFT JOIN user_roles ur ON ur.user_id=u.id LEFT JOIN roles r ON r.id=ur.role_id WHERE s.token_hash=?`, hash(v)).Scan(&u.ID, &u.Username, &disabled, &created, &exp, &u.Role)
	if err != nil {
		return User{}, err
	}
	t, _ := time.Parse(time.RFC3339Nano, exp)
	if disabled != 0 || time.Now().After(t) {
		return User{}, errors.New("session expired or disabled")
	}
	u.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	return u, nil
}
func (s *Store) DeleteSession(ctx context.Context, v string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM sessions WHERE token_hash=?", hash(v))
	return err
}

func (s *Store) CreateEnrollmentToken(ctx context.Context, userID string, ttl time.Duration, maxUses int) (Token, string, error) {
	if ttl <= 0 || maxUses < 1 {
		return Token{}, "", errors.New("invalid token constraints")
	}
	v, err := TokenValue()
	if err != nil {
		return Token{}, "", err
	}
	t := Token{ID: id("tok"), UserID: userID, ExpiresAt: time.Now().UTC().Add(ttl), MaxUses: maxUses, CreatedAt: time.Now().UTC()}
	_, err = s.db.ExecContext(ctx, "INSERT INTO enrollment_tokens(id,user_id,token_hash,expires_at,max_uses,created_at) VALUES(?,?,?,?,?,?)", t.ID, t.UserID, hash(v), t.ExpiresAt.Format(time.RFC3339Nano), t.MaxUses, t.CreatedAt.Format(time.RFC3339Nano))
	return t, v, err
}
func (s *Store) ListTokens(ctx context.Context) ([]Token, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id,user_id,expires_at,max_uses,uses,revoked,created_at FROM enrollment_tokens ORDER BY created_at DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Token
	for rows.Next() {
		var t Token
		var exp, created string
		var revoked int
		if err := rows.Scan(&t.ID, &t.UserID, &exp, &t.MaxUses, &t.Uses, &revoked, &created); err != nil {
			return nil, err
		}
		t.ExpiresAt, _ = time.Parse(time.RFC3339Nano, exp)
		t.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		t.Revoked = revoked != 0
		out = append(out, t)
	}
	return out, rows.Err()
}
func (s *Store) RevokeToken(ctx context.Context, tokenID string) error {
	_, err := s.db.ExecContext(ctx, "UPDATE enrollment_tokens SET revoked=1 WHERE id=?", tokenID)
	return err
}
func (s *Store) ConsumeToken(ctx context.Context, value string) (string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var tokenID, userID, exp string
	var max, uses, rev int
	err = tx.QueryRowContext(ctx, "SELECT id,user_id,expires_at,max_uses,uses,revoked FROM enrollment_tokens WHERE token_hash=?", hash(value)).Scan(&tokenID, &userID, &exp, &max, &uses, &rev)
	if err != nil {
		return "", errors.New("invalid enrollment token")
	}
	expires, _ := time.Parse(time.RFC3339Nano, exp)
	if rev != 0 || time.Now().After(expires) || uses >= max {
		return "", errors.New("enrollment token expired, revoked, or exhausted")
	}
	res, err := tx.ExecContext(ctx, "UPDATE enrollment_tokens SET uses=uses+1 WHERE id=? AND uses=?", tokenID, uses)
	if err != nil {
		return "", err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return "", errors.New("token concurrently consumed")
	}
	return userID, tx.Commit()
}

func (s *Store) nextIP(ctx context.Context, network string) (string, error) {
	ipnetIP, n, err := net.ParseCIDR(network)
	if err != nil {
		return "", err
	}
	rows, err := s.db.QueryContext(ctx, "SELECT virtual_ip FROM devices")
	if err != nil {
		return "", err
	}
	used := map[string]bool{}
	for rows.Next() {
		var ip string
		_ = rows.Scan(&ip)
		used[ip] = true
	}
	rows.Close()
	ip := append(net.IP(nil), ipnetIP.To4()...)
	inc(ip) // Reserve network+1 for the TyxNet server.
	for i := 0; i < 1<<20; i++ {
		inc(ip)
		if !n.Contains(ip) {
			break
		}
		last := ip[3]
		if last == 255 {
			continue
		}
		if !used[ip.String()] {
			return ip.String(), nil
		}
	}
	return "", errors.New("virtual network exhausted")
}
func inc(ip net.IP) {
	for j := len(ip) - 1; j >= 0; j-- {
		ip[j]++
		if ip[j] != 0 {
			return
		}
	}
}
func (s *Store) JoinDevice(ctx context.Context, token, name, osName, arch, version, network string, publicKey []byte) (Device, error) {
	userID, err := s.ConsumeToken(ctx, token)
	if err != nil {
		return Device{}, err
	}
	vip, err := s.nextIP(ctx, network)
	if err != nil {
		return Device{}, err
	}
	d := Device{ID: id("dev"), UserID: userID, Name: name, VirtualIP: vip, OS: osName, Arch: arch, Version: version, CreatedAt: time.Now().UTC()}
	_, err = s.db.ExecContext(ctx, "INSERT INTO devices(id,user_id,name,virtual_ip,public_key,os,arch,version,created_at) VALUES(?,?,?,?,?,?,?,?,?)", d.ID, d.UserID, d.Name, d.VirtualIP, publicKey, d.OS, d.Arch, d.Version, d.CreatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return Device{}, err
	}
	return d, nil
}
func (s *Store) ListDevices(ctx context.Context) ([]Device, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id,user_id,name,virtual_ip,os,arch,version,revoked,last_seen,created_at FROM devices ORDER BY created_at")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Device
	for rows.Next() {
		var d Device
		var rev int
		var seen sql.NullString
		var created string
		if err := rows.Scan(&d.ID, &d.UserID, &d.Name, &d.VirtualIP, &d.OS, &d.Arch, &d.Version, &rev, &seen, &created); err != nil {
			return nil, err
		}
		d.Revoked = rev != 0
		d.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		if seen.Valid {
			t, _ := time.Parse(time.RFC3339Nano, seen.String)
			d.LastSeen = &t
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
func (s *Store) RevokeDevice(ctx context.Context, deviceID string) error {
	_, err := s.db.ExecContext(ctx, "UPDATE devices SET revoked=1 WHERE id=?", deviceID)
	return err
}
func (s *Store) DevicePublicKey(ctx context.Context, deviceID string) ([]byte, error) {
	var key []byte
	var revoked int
	err := s.db.QueryRowContext(ctx, "SELECT public_key,revoked FROM devices WHERE id=?", deviceID).Scan(&key, &revoked)
	if err != nil {
		return nil, err
	}
	if revoked != 0 {
		return nil, errors.New("device revoked")
	}
	return key, nil
}
func (s *Store) Users(ctx context.Context) ([]User, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT u.id,u.username,u.disabled,u.created_at,COALESCE(r.name,'member') FROM users u LEFT JOIN user_roles ur ON ur.user_id=u.id LEFT JOIN roles r ON r.id=ur.role_id ORDER BY u.username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		var u User
		var disabled int
		var created string
		if err := rows.Scan(&u.ID, &u.Username, &disabled, &created, &u.Role); err != nil {
			return nil, err
		}
		u.Disabled = disabled != 0
		u.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		out = append(out, u)
	}
	return out, rows.Err()
}
func (s *Store) CreateUser(ctx context.Context, username, passwordHash, role string) (User, error) {
	roles := map[string]string{"admin": "role_admin", "operator": "role_operator", "member": "role_member", "viewer": "role_viewer"}
	roleID, ok := roles[role]
	if !ok {
		return User{}, errors.New("invalid role")
	}
	u := User{ID: id("usr"), Username: username, Role: role, CreatedAt: time.Now().UTC()}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "INSERT INTO users(id,username,password_hash,created_at) VALUES(?,?,?,?)", u.ID, u.Username, passwordHash, u.CreatedAt.Format(time.RFC3339Nano)); err != nil {
		return User{}, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO user_roles(user_id,role_id) VALUES(?,?)", u.ID, roleID); err != nil {
		return User{}, err
	}
	return u, tx.Commit()
}
func (s *Store) DeleteUser(ctx context.Context, userID string) error {
	var n int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM devices WHERE user_id=?", userID).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return errors.New("user still owns devices")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, q := range []string{"DELETE FROM sessions WHERE user_id=?", "DELETE FROM enrollment_tokens WHERE user_id=?", "DELETE FROM user_roles WHERE user_id=?", "DELETE FROM users WHERE id=?"} {
		if _, err = tx.ExecContext(ctx, q, userID); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func (s *Store) CreateCommand(ctx context.Context, sender, device, typ string, ttl time.Duration) (Command, error) {
	allowed := map[string]bool{"system.restart": true, "system.shutdown": true, "client.reconnect": true, "client.status": true, "client.update-check": true, "logs.collect": true}
	if !allowed[typ] {
		return Command{}, errors.New("command type is not allowed")
	}
	if ttl <= 0 || ttl > 5*time.Minute {
		return Command{}, errors.New("command TTL must be between 0 and 5m")
	}
	c := Command{ID: id("cmd"), SenderUserID: sender, DeviceID: device, Type: typ, Status: "queued", CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(ttl)}
	_, err := s.db.ExecContext(ctx, "INSERT INTO commands(id,sender_user_id,device_id,type,status,created_at,expires_at) VALUES(?,?,?,?,?,?,?)", c.ID, c.SenderUserID, c.DeviceID, c.Type, c.Status, c.CreatedAt.Format(time.RFC3339Nano), c.ExpiresAt.Format(time.RFC3339Nano))
	return c, err
}
func (s *Store) Audit(ctx context.Context, actor, action, target, remoteIP, detail string) {
	_, _ = s.db.ExecContext(ctx, "INSERT INTO audit_logs(actor_id,action,target_id,remote_ip,detail,created_at) VALUES(?,?,?,?,?,?)", actor, action, target, remoteIP, detail, time.Now().UTC().Format(time.RFC3339Nano))
}
