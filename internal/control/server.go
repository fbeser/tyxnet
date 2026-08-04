package control

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/fbeser/tyxnet/internal/auth"
	"github.com/fbeser/tyxnet/internal/storage"
)

type Server struct {
	store       *storage.Store
	network     string
	ttl         time.Duration
	started     time.Time
	log         *slog.Logger
	limiter     *ipLimiter
	challengeMu sync.Mutex
	challenges  map[string]challenge
}
type challenge struct {
	nonce   []byte
	expires time.Time
}
type ipLimiter struct {
	mu   sync.Mutex
	hits map[string][]time.Time
}
type ctxKey string

const userKey ctxKey = "user"

func New(store *storage.Store, network string, ttl time.Duration, log *slog.Logger) *Server {
	return &Server{store: store, network: network, ttl: ttl, started: time.Now(), log: log, limiter: &ipLimiter{hits: map[string][]time.Time{}}, challenges: map[string]challenge{}}
}
func (s *Server) Handler() http.Handler {
	m := http.NewServeMux()
	m.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { write(w, 200, map[string]string{"status": "ok"}) })
	m.HandleFunc("POST /api/v1/auth/login", s.login)
	m.HandleFunc("POST /api/v1/enroll", s.enroll)
	m.HandleFunc("POST /control/v1/challenge", s.deviceChallenge)
	m.HandleFunc("POST /control/v1/connect", s.deviceConnect)
	m.HandleFunc("GET /{$}", s.dashboard)
	m.Handle("/api/v1/", s.authorized(http.HandlerFunc(s.api)))
	return securityHeaders(s.rateLimit(m))
}
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'unsafe-inline'")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}
func (s *Server) rateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, _ := net.SplitHostPort(r.RemoteAddr)
		now := time.Now()
		s.limiter.mu.Lock()
		xs := s.limiter.hits[host][:0]
		for _, x := range s.limiter.hits[host] {
			if now.Sub(x) < time.Minute {
				xs = append(xs, x)
			}
		}
		if len(xs) >= 120 {
			s.limiter.hits[host] = xs
			s.limiter.mu.Unlock()
			problem(w, 429, "rate_limited", "too many requests")
			return
		}
		s.limiter.hits[host] = append(xs, now)
		s.limiter.mu.Unlock()
		next.ServeHTTP(w, r)
	})
}
func decode(r *http.Request, v any) error {
	d := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return err
	}
	if err := d.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request must contain one JSON value")
	}
	return nil
}
func write(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func problem(w http.ResponseWriter, status int, code, msg string) {
	write(w, status, map[string]any{"error": map[string]string{"code": code, "message": msg}})
}
func bearer(r *http.Request) string {
	p := strings.Fields(r.Header.Get("Authorization"))
	if len(p) == 2 && strings.EqualFold(p[0], "Bearer") {
		return p[1]
	}
	return ""
}
func (s *Server) authorized(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, err := s.store.SessionUser(r.Context(), bearer(r))
		if err != nil {
			problem(w, 401, "unauthorized", "valid bearer token required")
			return
		}
		r = r.WithContext(contextWithUser(r.Context(), u))
		next.ServeHTTP(w, r)
	})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if decode(r, &in) != nil {
		problem(w, 400, "invalid_request", "invalid JSON")
		return
	}
	u, ph, err := s.store.Authenticate(r.Context(), in.Username)
	if err != nil || !auth.VerifyPassword(ph, in.Password) || u.Disabled {
		s.store.Audit(r.Context(), "", "login.failed", in.Username, r.RemoteAddr, "")
		time.Sleep(100 * time.Millisecond)
		problem(w, 401, "invalid_credentials", "invalid credentials")
		return
	}
	token, err := s.store.CreateSession(r.Context(), u.ID, s.ttl)
	if err != nil {
		problem(w, 500, "internal", "session creation failed")
		return
	}
	s.store.Audit(r.Context(), u.ID, "login", "", r.RemoteAddr, "")
	write(w, 200, map[string]any{"access_token": token, "expires_in": int(s.ttl.Seconds()), "user": u})
}
func (s *Server) enroll(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token, Name, OS, Arch, Version string
		PublicKey                      []byte `json:"public_key"`
	}
	if decode(r, &in) != nil || in.Name == "" || len(in.PublicKey) != ed25519.PublicKeySize {
		problem(w, 400, "invalid_enrollment", "name and Ed25519 public_key are required")
		return
	}
	d, err := s.store.JoinDevice(r.Context(), in.Token, in.Name, in.OS, in.Arch, in.Version, s.network, in.PublicKey)
	if err != nil {
		problem(w, 403, "enrollment_rejected", err.Error())
		return
	}
	s.store.Audit(r.Context(), d.UserID, "device.join", d.ID, r.RemoteAddr, "")
	write(w, 201, d)
}
func (s *Server) deviceChallenge(w http.ResponseWriter, r *http.Request) {
	var in struct {
		DeviceID string `json:"device_id"`
	}
	if decode(r, &in) != nil {
		problem(w, 400, "invalid_request", "device_id required")
		return
	}
	if _, err := s.store.DevicePublicKey(r.Context(), in.DeviceID); err != nil {
		problem(w, 403, "unknown_device", "device is unknown or revoked")
		return
	}
	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		problem(w, 500, "internal", "random generation failed")
		return
	}
	s.challengeMu.Lock()
	s.challenges[in.DeviceID] = challenge{nonce: nonce, expires: time.Now().Add(30 * time.Second)}
	s.challengeMu.Unlock()
	write(w, 200, map[string]string{"challenge": base64.RawStdEncoding.EncodeToString(nonce)})
}
func (s *Server) deviceConnect(w http.ResponseWriter, r *http.Request) {
	var in struct{ DeviceID, Signature string }
	if decode(r, &in) != nil {
		problem(w, 400, "invalid_request", "invalid proof")
		return
	}
	s.challengeMu.Lock()
	c, ok := s.challenges[in.DeviceID]
	delete(s.challenges, in.DeviceID)
	s.challengeMu.Unlock()
	key, err := s.store.DevicePublicKey(r.Context(), in.DeviceID)
	sig, e := base64.RawStdEncoding.DecodeString(in.Signature)
	if !ok || time.Now().After(c.expires) || err != nil || e != nil || !ed25519.Verify(key, c.nonce, sig) {
		problem(w, 403, "authentication_failed", "invalid device proof")
		return
	}
	s.store.Audit(r.Context(), in.DeviceID, "device.connect", in.DeviceID, r.RemoteAddr, "")
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	f, ok := w.(http.Flusher)
	if !ok {
		problem(w, 500, "stream_unsupported", "streaming unsupported")
		return
	}
	fmt.Fprint(w, "event: connected\ndata: {}\n\n")
	f.Flush()
	ticker := time.NewTicker(25 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			fmt.Fprint(w, "event: keepalive\ndata: {}\n\n")
			f.Flush()
		}
	}
}
func (s *Server) api(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/")
	switch {
	case r.Method == "POST" && path == "auth/logout":
		_ = s.store.DeleteSession(r.Context(), bearer(r))
		w.WriteHeader(204)
	case r.Method == "GET" && path == "auth/me":
		write(w, 200, u)
	case r.Method == "GET" && path == "devices":
		if !permit(w, u, "device.view") {
			return
		}
		v, err := s.store.ListDevices(r.Context())
		respond(w, v, err)
	case r.Method == "GET" && path == "users":
		if !permit(w, u, "user.view") {
			return
		}
		v, err := s.store.Users(r.Context())
		respond(w, v, err)
	case r.Method == "POST" && path == "users":
		if !permit(w, u, "user.create") {
			return
		}
		s.createUser(w, r, u)
	case r.Method == "DELETE" && strings.HasPrefix(path, "users/"):
		if !permit(w, u, "user.delete") {
			return
		}
		id := strings.TrimPrefix(path, "users/")
		if err := s.store.DeleteUser(r.Context(), id); err != nil {
			problem(w, 409, "delete_failed", err.Error())
			return
		}
		s.store.Audit(r.Context(), u.ID, "user.delete", id, r.RemoteAddr, "")
		w.WriteHeader(204)
	case r.Method == "GET" && path == "tokens":
		if !permit(w, u, "token.view") {
			return
		}
		v, err := s.store.ListTokens(r.Context())
		respond(w, v, err)
	case r.Method == "POST" && path == "tokens":
		if !permit(w, u, "token.create") {
			return
		}
		s.createToken(w, r, u)
	case r.Method == "DELETE" && strings.HasPrefix(path, "tokens/"):
		if !permit(w, u, "token.revoke") {
			return
		}
		id := strings.TrimPrefix(path, "tokens/")
		if err := s.store.RevokeToken(r.Context(), id); err != nil {
			problem(w, 500, "internal", err.Error())
			return
		}
		s.store.Audit(r.Context(), u.ID, "token.revoke", id, r.RemoteAddr, "")
		w.WriteHeader(204)
	case r.Method == "DELETE" && strings.HasPrefix(path, "devices/"):
		if !permit(w, u, "device.revoke") {
			return
		}
		id := strings.TrimPrefix(path, "devices/")
		if err := s.store.RevokeDevice(r.Context(), id); err != nil {
			problem(w, 500, "internal", err.Error())
			return
		}
		s.store.Audit(r.Context(), u.ID, "device.revoke", id, r.RemoteAddr, "")
		w.WriteHeader(204)
	case r.Method == "GET" && path == "server/status":
		ds, _ := s.store.ListDevices(r.Context())
		us, _ := s.store.Users(r.Context())
		write(w, 200, map[string]any{"uptime_seconds": int(time.Since(s.started).Seconds()), "devices": len(ds), "users": len(us)})
	case r.Method == "POST" && (strings.HasSuffix(path, "/restart") || strings.HasSuffix(path, "/shutdown") || strings.HasSuffix(path, "/disconnect")):
		s.command(w, r, u, path)
	default:
		problem(w, 404, "not_found", "endpoint not found")
	}
}
func (s *Server) createUser(w http.ResponseWriter, r *http.Request, actor storage.User) {
	var in struct{ Username, Password, Role string }
	if decode(r, &in) != nil {
		problem(w, 400, "invalid_request", "invalid JSON")
		return
	}
	ph, err := auth.HashPassword(in.Password)
	if err != nil {
		problem(w, 400, "invalid_password", err.Error())
		return
	}
	u, err := s.store.CreateUser(r.Context(), in.Username, ph, in.Role)
	if err != nil {
		problem(w, 400, "user_failed", err.Error())
		return
	}
	s.store.Audit(r.Context(), actor.ID, "user.create", u.ID, r.RemoteAddr, "")
	write(w, 201, u)
}
func (s *Server) createToken(w http.ResponseWriter, r *http.Request, u storage.User) {
	var in struct {
		UserID  string `json:"user_id"`
		Expires string `json:"expires"`
		MaxUses int    `json:"max_uses"`
	}
	if decode(r, &in) != nil {
		problem(w, 400, "invalid_request", "invalid JSON")
		return
	}
	ttl, err := time.ParseDuration(in.Expires)
	if err != nil {
		problem(w, 400, "invalid_duration", "expires must be a duration")
		return
	}
	t, v, err := s.store.CreateEnrollmentToken(r.Context(), in.UserID, ttl, in.MaxUses)
	if err != nil {
		problem(w, 400, "token_failed", err.Error())
		return
	}
	s.store.Audit(r.Context(), u.ID, "token.create", t.ID, r.RemoteAddr, "")
	write(w, 201, map[string]any{"token": v, "metadata": t})
}
func (s *Server) command(w http.ResponseWriter, r *http.Request, u storage.User, path string) {
	parts := strings.Split(path, "/")
	if len(parts) != 3 {
		problem(w, 404, "not_found", "endpoint not found")
		return
	}
	typ := "client." + parts[2]
	perm := "device." + parts[2]
	if parts[2] == "restart" || parts[2] == "shutdown" {
		typ = "system." + parts[2]
	}
	if !permit(w, u, perm) {
		return
	}
	if parts[2] == "disconnect" {
		typ = "client.reconnect"
	}
	c, err := s.store.CreateCommand(r.Context(), u.ID, parts[1], typ, 2*time.Minute)
	if err != nil {
		problem(w, 400, "command_failed", err.Error())
		return
	}
	s.store.Audit(r.Context(), u.ID, typ, parts[1], r.RemoteAddr, "")
	write(w, 202, c)
}
func respond(w http.ResponseWriter, v any, err error) {
	if err != nil {
		problem(w, 500, "internal", err.Error())
		return
	}
	write(w, 200, v)
}
func permit(w http.ResponseWriter, u storage.User, p string) bool {
	if !auth.Allowed(auth.Role(u.Role), p) {
		problem(w, 403, "forbidden", "permission denied")
		return false
	}
	return true
}

var dashboardTemplate = template.Must(template.New("dashboard").Parse(`<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>TyxNet</title><style>body{font:16px system-ui;margin:3rem;max-width:900px;background:#101827;color:#e8eef8}.cards{display:flex;gap:1rem}.card{background:#1d2a3d;padding:1.4rem;border-radius:12px;min-width:140px}small{color:#9fb0c8}</style></head><body><h1>TyxNet</h1><p>Central virtual network management</p><div class="cards"><div class="card"><small>Devices</small><h2>{{.Devices}}</h2></div><div class="card"><small>Users</small><h2>{{.Users}}</h2></div><div class="card"><small>Uptime</small><h2>{{.Uptime}}</h2></div></div><p><small>Management operations require authentication through /api/v1.</small></p></body></html>`))

func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	ds, _ := s.store.ListDevices(r.Context())
	us, _ := s.store.Users(r.Context())
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = dashboardTemplate.Execute(w, map[string]any{"Devices": len(ds), "Users": len(us), "Uptime": time.Since(s.started).Round(time.Second)})
}
func (s *Server) ListenAndServe(addr, cert, key string) error {
	srv := &http.Server{Addr: addr, Handler: s.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
	if cert != "" && key != "" {
		return srv.ListenAndServeTLS(cert, key)
	}
	s.log.Warn("TLS disabled; use only behind a trusted TLS reverse proxy")
	return srv.ListenAndServe()
}

// context helpers are kept local to avoid exposing authentication internals.
func contextWithUser(ctx context.Context, u storage.User) context.Context {
	return context.WithValue(ctx, userKey, u)
}
func userFrom(r *http.Request) storage.User { return r.Context().Value(userKey).(storage.User) }
