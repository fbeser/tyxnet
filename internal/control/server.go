package control

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fbeser/tyxnet/internal/application"
	"github.com/fbeser/tyxnet/internal/auth"
	"github.com/fbeser/tyxnet/internal/dataplane"
	"github.com/fbeser/tyxnet/internal/routing"
	"github.com/fbeser/tyxnet/internal/storage"
	"github.com/fbeser/tyxnet/pkg/protocol"
)

type Server struct {
	store            *storage.Store
	network          string
	ttl              time.Duration
	started          time.Time
	log              *slog.Logger
	limiter          *ipLimiter
	challengeMu      sync.Mutex
	challenges       map[string]challenge
	connectionsMu    sync.RWMutex
	connections      map[string]int
	localBootstrap   bool
	remoteBootstrap  bool
	adapterName      string
	adapterAddress   string
	pingIntervalNS   atomic.Int64
	startupSpec      application.StartupSpec
	trayToken        string
	shutdown         func()
	startupAvailable func() (bool, string)
	startupEnabled   func(application.StartupSpec) (bool, error)
	setStartup       func(application.StartupSpec, bool) error
	traffic          *routing.TrafficMonitor
	dataPlane        *dataplane.Server
	commandMu        sync.Mutex
	commandSignals   map[string]chan struct{}
}

func (s *Server) SetAdapter(name, address string) {
	s.adapterName = name
	s.adapterAddress = address
}

func (s *Server) SetDataPlane(dataPlane *dataplane.Server) { s.dataPlane = dataPlane }

func (s *Server) ConfigureApplication(spec application.StartupSpec, trayToken string, shutdown func()) {
	s.startupSpec = spec
	s.trayToken = trayToken
	s.shutdown = shutdown
}

// AllowRemoteBootstrap permits first-admin setup from the listening network.
// Callers must require an explicit operator opt-in before enabling it.
func (s *Server) AllowRemoteBootstrap() { s.remoteBootstrap = true }

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

const (
	sessionCookieName  = "tyxnet_session"
	rememberSessionTTL = 30 * 24 * time.Hour
)

func New(store *storage.Store, network string, ttl time.Duration, log *slog.Logger, localBootstrap bool) *Server {
	s := &Server{store: store, network: network, ttl: ttl, started: time.Now(), log: log, limiter: &ipLimiter{hits: map[string][]time.Time{}}, challenges: map[string]challenge{}, connections: map[string]int{}, localBootstrap: localBootstrap, startupAvailable: application.StartupAvailable, startupEnabled: application.StartupEnabled, setStartup: application.SetStartup, traffic: routing.NewTrafficMonitor(), commandSignals: map[string]chan struct{}{}}
	s.pingIntervalNS.Store(int64(25 * time.Second))
	if value, err := store.Setting(context.Background(), "ping_interval"); err == nil {
		if interval, parseErr := time.ParseDuration(value); parseErr == nil && validPingInterval(interval) {
			s.pingIntervalNS.Store(int64(interval))
		}
	}
	return s
}

func (s *Server) TrafficMonitor() *routing.TrafficMonitor { return s.traffic }

func (s *Server) commandSignal(deviceID string) chan struct{} {
	s.commandMu.Lock()
	defer s.commandMu.Unlock()
	if s.commandSignals[deviceID] == nil {
		s.commandSignals[deviceID] = make(chan struct{}, 1)
	}
	return s.commandSignals[deviceID]
}

func (s *Server) notifyCommand(deviceID string) {
	signal := s.commandSignal(deviceID)
	select {
	case signal <- struct{}{}:
	default:
	}
}

func validPingInterval(interval time.Duration) bool {
	return interval >= 5*time.Second && interval <= time.Hour
}

func (s *Server) pingInterval() time.Duration { return time.Duration(s.pingIntervalNS.Load()) }

func (s *Server) deviceConnected(deviceID string) {
	s.connectionsMu.Lock()
	s.connections[deviceID]++
	s.connectionsMu.Unlock()
}

func (s *Server) deviceDisconnected(deviceID string) {
	s.connectionsMu.Lock()
	if s.connections[deviceID] <= 1 {
		delete(s.connections, deviceID)
	} else {
		s.connections[deviceID]--
	}
	s.connectionsMu.Unlock()
}

func (s *Server) deviceOnline(deviceID string) bool {
	s.connectionsMu.RLock()
	online := s.connections[deviceID] > 0
	s.connectionsMu.RUnlock()
	return online
}

func (s *Server) setDevicePresence(devices []storage.Device) {
	for i := range devices {
		devices[i].Online = !devices[i].Revoked && s.deviceOnline(devices[i].ID)
	}
}

func (s *Server) SetDefaultPingInterval(interval time.Duration) {
	if _, err := s.store.Setting(context.Background(), "ping_interval"); err == nil {
		return
	}
	if validPingInterval(interval) {
		s.pingIntervalNS.Store(int64(interval))
	}
}

//go:embed web/*
var embeddedWeb embed.FS

func (s *Server) Handler() http.Handler {
	m := http.NewServeMux()
	m.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { write(w, 200, map[string]string{"status": "ok"}) })
	m.HandleFunc("POST /api/v1/auth/login", s.login)
	m.HandleFunc("GET /api/v1/setup/status", s.setupStatus)
	m.HandleFunc("POST /api/v1/setup", s.setup)
	m.HandleFunc("POST /api/v1/enroll", s.enroll)
	m.HandleFunc("POST /control/v1/challenge", s.deviceChallenge)
	m.HandleFunc("POST /control/v1/connect", s.deviceConnect)
	m.HandleFunc("POST /control/v1/commands/{id}/result", s.deviceCommandResult)
	m.HandleFunc("GET /api/tray", s.trayStatus)
	m.HandleFunc("POST /api/tray/startup", s.trayStartup)
	m.HandleFunc("POST /api/tray/quit", s.trayQuit)
	m.HandleFunc("GET /{$}", s.dashboard)
	assets, _ := fs.Sub(embeddedWeb, "web")
	assetHandler := http.StripPrefix("/ui/", http.FileServer(http.FS(assets)))
	m.Handle("GET /ui/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		assetHandler.ServeHTTP(w, r)
	}))
	m.Handle("/api/v1/", s.authorized(http.HandlerFunc(s.api)))
	return securityHeaders(s.rateLimit(m))
}

func (s *Server) trayAllowed(r *http.Request) bool {
	provided := r.Header.Get("X-TyxNet-Tray-Token")
	return remoteIsLoopback(r.RemoteAddr) && s.trayToken != "" && subtle.ConstantTimeCompare([]byte(provided), []byte(s.trayToken)) == 1
}

func (s *Server) trayStatus(w http.ResponseWriter, r *http.Request) {
	if !s.trayAllowed(r) {
		problem(w, 403, "forbidden", "local tray authentication required")
		return
	}
	available, reason := s.startupAvailable()
	enabled := false
	if available {
		var err error
		enabled, err = s.startupEnabled(s.startupSpec)
		if err != nil {
			problem(w, 500, "startup_status_failed", err.Error())
			return
		}
	}
	write(w, 200, map[string]any{"running": true, "startup_available": available, "startup_enabled": enabled, "startup_reason": reason})
}

func (s *Server) trayStartup(w http.ResponseWriter, r *http.Request) {
	if !s.trayAllowed(r) {
		problem(w, 403, "forbidden", "local tray authentication required")
		return
	}
	s.updateStartup(w, r, storage.User{ID: "local-tray", Role: "admin"})
}

func (s *Server) trayQuit(w http.ResponseWriter, r *http.Request) {
	if !s.trayAllowed(r) {
		problem(w, 403, "forbidden", "local tray authentication required")
		return
	}
	w.WriteHeader(http.StatusAccepted)
	if s.shutdown != nil {
		go func() {
			time.Sleep(100 * time.Millisecond)
			s.shutdown()
		}()
	}
}

func (s *Server) updateStartup(w http.ResponseWriter, r *http.Request, actor storage.User) {
	var in struct {
		Enabled bool `json:"enabled"`
	}
	if decode(r, &in) != nil {
		problem(w, 400, "invalid_request", "enabled is required")
		return
	}
	if available, reason := s.startupAvailable(); !available {
		problem(w, 409, "startup_unavailable", reason)
		return
	}
	if err := s.setStartup(s.startupSpec, in.Enabled); err != nil {
		problem(w, 500, "startup_update_failed", err.Error())
		return
	}
	s.store.Audit(r.Context(), actor.ID, "server.startup.update", "server", r.RemoteAddr, fmt.Sprint(in.Enabled))
	write(w, 200, map[string]bool{"enabled": in.Enabled})
}
func remoteIsLoopback(remote string) bool {
	host, _, err := net.SplitHostPort(remote)
	ip := net.ParseIP(host)
	return err == nil && ip != nil && ip.IsLoopback()
}
func (s *Server) setupStatus(w http.ResponseWriter, r *http.Request) {
	n, err := s.store.UserCount(r.Context())
	if err != nil {
		problem(w, 500, "internal", "setup status failed")
		return
	}
	write(w, 200, map[string]any{"required": n == 0, "enabled": s.bootstrapAllowed(r)})
}
func (s *Server) setup(w http.ResponseWriter, r *http.Request) {
	if !s.bootstrapAllowed(r) {
		problem(w, 403, "setup_disabled", "initial web setup is not enabled for this request")
		return
	}
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Remember bool   `json:"remember,omitempty"`
	}
	if decode(r, &in) != nil || strings.TrimSpace(in.Username) == "" {
		problem(w, 400, "invalid_request", "username and password are required")
		return
	}
	ph, err := auth.HashPassword(in.Password)
	if err != nil {
		problem(w, 400, "invalid_password", err.Error())
		return
	}
	u, err := s.store.CreateInitialAdmin(r.Context(), strings.TrimSpace(in.Username), ph)
	if err != nil {
		problem(w, 409, "setup_complete", err.Error())
		return
	}
	token, err := s.store.CreateSession(r.Context(), u.ID, s.ttl)
	if err != nil {
		problem(w, 500, "internal", "session creation failed")
		return
	}
	s.store.Audit(r.Context(), u.ID, "setup.complete", u.ID, r.RemoteAddr, "")
	write(w, 201, map[string]any{"access_token": token, "expires_in": int(s.ttl.Seconds()), "user": u})
}

func (s *Server) bootstrapAllowed(r *http.Request) bool {
	return s.localBootstrap && (s.remoteBootstrap || remoteIsLoopback(r.RemoteAddr))
}
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'")
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
func authToken(r *http.Request) string {
	if token := bearer(r); token != "" {
		return token
	}
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return ""
	}
	return cookie.Value
}
func secureRequest(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}
func (s *Server) authorized(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, err := s.store.SessionUser(r.Context(), authToken(r))
		if err != nil {
			problem(w, 401, "unauthorized", "valid session required")
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
		Remember bool   `json:"remember"`
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
	ttl := s.ttl
	if in.Remember {
		if !secureRequest(r) {
			problem(w, 400, "https_required", "remember me requires HTTPS")
			return
		}
		ttl = rememberSessionTTL
	}
	token, err := s.store.CreateSession(r.Context(), u.ID, ttl)
	if err != nil {
		problem(w, 500, "internal", "session creation failed")
		return
	}
	if in.Remember {
		http.SetCookie(w, &http.Cookie{Name: sessionCookieName, Value: token, Path: "/", MaxAge: int(ttl.Seconds()), Expires: time.Now().Add(ttl), HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode})
	}
	s.store.Audit(r.Context(), u.ID, "login", "", r.RemoteAddr, "")
	write(w, 200, map[string]any{"access_token": token, "expires_in": int(ttl.Seconds()), "user": u})
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
	_ = s.store.TouchDevice(r.Context(), in.DeviceID)
	s.store.Audit(r.Context(), in.DeviceID, "device.connect", in.DeviceID, r.RemoteAddr, "")
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	f, ok := w.(http.Flusher)
	if !ok {
		problem(w, 500, "stream_unsupported", "streaming unsupported")
		return
	}
	device, err := s.store.Device(r.Context(), in.DeviceID)
	if err != nil {
		problem(w, 500, "internal", "device state unavailable")
		return
	}
	var dataBootstrap *dataplane.Bootstrap
	if s.dataPlane != nil && secureRequest(r) {
		bootstrap, bootstrapErr := s.dataPlane.Register(device.ID, net.ParseIP(device.VirtualIP))
		if bootstrapErr != nil {
			problem(w, 500, "data_plane_failed", "data-plane session unavailable")
			return
		}
		dataBootstrap = &bootstrap
		defer s.dataPlane.Remove(device.ID, bootstrap.SessionID)
	}
	s.deviceConnected(in.DeviceID)
	defer s.deviceDisconnected(in.DeviceID)
	connected, _ := json.Marshal(map[string]any{"protocol_version": 1, "virtual_ip": device.VirtualIP, "virtual_network": s.network, "ping_interval_seconds": int(s.pingInterval().Seconds()), "data_plane": dataBootstrap})
	if _, err := fmt.Fprintf(w, "event: connected\ndata: %s\n\n", connected); err != nil {
		return
	}
	if err := s.writePendingCommands(r.Context(), w, in.DeviceID); err != nil {
		return
	}
	f.Flush()
	pingTimer := time.NewTimer(s.pingInterval())
	commandTicker := time.NewTicker(2 * time.Second)
	defer pingTimer.Stop()
	defer commandTicker.Stop()
	commandSignal := s.commandSignal(in.DeviceID)
	var dataExpiry <-chan time.Time
	if dataBootstrap != nil {
		expiryTimer := time.NewTimer(time.Until(dataBootstrap.ExpiresAt))
		defer expiryTimer.Stop()
		dataExpiry = expiryTimer.C
	}
	for {
		select {
		case <-r.Context().Done():
			return
		case <-dataExpiry:
			return
		case <-pingTimer.C:
			current, err := s.store.Device(r.Context(), in.DeviceID)
			if err != nil {
				return
			}
			if current.VirtualIP != device.VirtualIP {
				return
			}
			payload, _ := json.Marshal(map[string]any{"protocol_version": 1, "virtual_ip": current.VirtualIP, "virtual_network": s.network, "ping_interval_seconds": int(s.pingInterval().Seconds())})
			if _, err := fmt.Fprintf(w, "event: ping\ndata: %s\n\n", payload); err != nil {
				return
			}
			f.Flush()
			_ = s.store.TouchDevice(r.Context(), in.DeviceID)
			pingTimer.Reset(s.pingInterval())
		case <-commandTicker.C:
			if err := s.writePendingCommands(r.Context(), w, in.DeviceID); err != nil {
				return
			}
			f.Flush()
		case <-commandSignal:
			if err := s.writePendingCommands(r.Context(), w, in.DeviceID); err != nil {
				return
			}
			f.Flush()
		}
	}
}

func (s *Server) writePendingCommands(ctx context.Context, w io.Writer, deviceID string) error {
	commands, err := s.store.PendingCommandsForDevice(ctx, deviceID, 20)
	if err != nil {
		return err
	}
	for _, command := range commands {
		if err := s.store.MarkCommandDelivered(ctx, command.ID, deviceID); err != nil {
			continue
		}
		payload, err := json.Marshal(protocol.ControlCommand{ProtocolVersion: protocol.Version, ID: command.ID, Type: command.Type, CreatedAt: command.CreatedAt, ExpiresAt: command.ExpiresAt})
		if err != nil {
			return err
		}
		if _, err = fmt.Fprintf(w, "event: command\ndata: %s\n\n", payload); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) deviceCommandResult(w http.ResponseWriter, r *http.Request) {
	commandID := r.PathValue("id")
	var result protocol.CommandResult
	if decode(r, &result) != nil || commandID == "" || len(result.Result) > 4096 || len(result.Error) > 4096 {
		problem(w, 400, "invalid_command_result", "invalid command result")
		return
	}
	s.challengeMu.Lock()
	challenge, ok := s.challenges[result.DeviceID]
	delete(s.challenges, result.DeviceID)
	s.challengeMu.Unlock()
	key, keyErr := s.store.DevicePublicKey(r.Context(), result.DeviceID)
	signature, signatureErr := base64.RawStdEncoding.DecodeString(result.Signature)
	payload, payloadErr := protocol.CommandResultSigningPayload(challenge.nonce, result.DeviceID, commandID, result.Status, result.Result, result.Error)
	if !ok || time.Now().After(challenge.expires) || keyErr != nil || signatureErr != nil || payloadErr != nil || !ed25519.Verify(key, payload, signature) {
		problem(w, 403, "authentication_failed", "invalid device proof")
		return
	}
	if err := s.store.UpdateCommandResult(r.Context(), commandID, result.DeviceID, result.Status, result.Result, result.Error); err != nil {
		problem(w, 409, "command_result_rejected", err.Error())
		return
	}
	s.store.Audit(r.Context(), result.DeviceID, "command."+result.Status, commandID, r.RemoteAddr, "")
	write(w, 200, map[string]string{"status": result.Status})
}
func (s *Server) api(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/")
	switch {
	case r.Method == "POST" && path == "auth/logout":
		_ = s.store.DeleteSession(r.Context(), authToken(r))
		http.SetCookie(w, &http.Cookie{Name: sessionCookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: secureRequest(r), SameSite: http.SameSiteStrictMode})
		w.WriteHeader(204)
	case r.Method == "GET" && path == "auth/me":
		write(w, 200, u)
	case r.Method == "GET" && path == "devices":
		if !permit(w, u, "device.view") {
			return
		}
		var v []storage.Device
		var err error
		if auth.Role(u.Role) == auth.Member {
			v, err = s.store.ListDevicesByUser(r.Context(), u.ID)
		} else {
			v, err = s.store.ListDevices(r.Context())
		}
		s.setDevicePresence(v)
		respond(w, v, err)
	case r.Method == "PATCH" && strings.HasPrefix(path, "devices/"):
		if !permit(w, u, "device.rename") {
			return
		}
		id := strings.TrimPrefix(path, "devices/")
		var in struct {
			Name      *string
			VirtualIP *string
		}
		if decode(r, &in) != nil {
			problem(w, 400, "invalid_request", "invalid JSON")
			return
		}
		if in.Name == nil && in.VirtualIP == nil {
			problem(w, 400, "invalid_request", "name or virtual IP is required")
			return
		}
		if in.VirtualIP != nil && !auth.Allowed(auth.Role(u.Role), "server.configure") {
			problem(w, 403, "forbidden", "role does not allow static IP assignment")
			return
		}
		if in.Name != nil {
			if err := s.store.RenameDevice(r.Context(), id, *in.Name); err != nil {
				problem(w, 400, "rename_failed", err.Error())
				return
			}
			s.store.Audit(r.Context(), u.ID, "device.rename", id, r.RemoteAddr, "")
		}
		if in.VirtualIP != nil {
			serverIP := strings.SplitN(s.adapterAddress, "/", 2)[0]
			if err := s.store.SetDeviceVirtualIP(r.Context(), id, *in.VirtualIP, s.network, serverIP); err != nil {
				problem(w, 400, "ip_assignment_failed", err.Error())
				return
			}
			s.store.Audit(r.Context(), u.ID, "device.ip.assign", id, r.RemoteAddr, *in.VirtualIP)
		}
		write(w, 200, map[string]string{"status": "updated"})
	case r.Method == "GET" && path == "server/settings":
		if !permit(w, u, "server.configure") {
			return
		}
		write(w, 200, map[string]any{"ping_interval_seconds": int(s.pingInterval().Seconds())})
	case r.Method == "PATCH" && path == "server/settings":
		if !permit(w, u, "server.configure") {
			return
		}
		var in struct {
			PingIntervalSeconds int `json:"ping_interval_seconds"`
		}
		if decode(r, &in) != nil {
			problem(w, 400, "invalid_request", "invalid JSON")
			return
		}
		interval := time.Duration(in.PingIntervalSeconds) * time.Second
		if !validPingInterval(interval) {
			problem(w, 400, "invalid_ping_interval", "ping interval must be between 5 and 3600 seconds")
			return
		}
		if err := s.store.SetSetting(r.Context(), "ping_interval", interval.String()); err != nil {
			problem(w, 500, "internal", "settings could not be saved")
			return
		}
		s.pingIntervalNS.Store(int64(interval))
		s.store.Audit(r.Context(), u.ID, "server.ping_interval.update", "server", r.RemoteAddr, interval.String())
		write(w, 200, map[string]any{"ping_interval_seconds": in.PingIntervalSeconds})
	case r.Method == "GET" && path == "server/startup":
		if !permit(w, u, "server.configure") {
			return
		}
		available, reason := s.startupAvailable()
		enabled := false
		if available {
			var err error
			enabled, err = s.startupEnabled(s.startupSpec)
			if err != nil {
				problem(w, 500, "startup_status_failed", err.Error())
				return
			}
		}
		write(w, 200, map[string]any{"available": available, "enabled": enabled, "reason": reason})
	case r.Method == "PATCH" && path == "server/startup":
		if !permit(w, u, "server.configure") {
			return
		}
		s.updateStartup(w, r, u)
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
	case r.Method == "PUT" && strings.HasPrefix(path, "users/") && strings.HasSuffix(path, "/password"):
		if !permit(w, u, "user.password.update") {
			return
		}
		parts := strings.Split(path, "/")
		if len(parts) != 3 || parts[1] == "" || parts[2] != "password" {
			problem(w, 404, "not_found", "endpoint not found")
			return
		}
		s.updateUserPassword(w, r, u, parts[1])
	case r.Method == "DELETE" && strings.HasPrefix(path, "users/"):
		if !permit(w, u, "user.delete") {
			return
		}
		id := strings.TrimPrefix(path, "users/")
		if id == u.ID {
			problem(w, 400, "self_delete", "you cannot delete your active account")
			return
		}
		if err := s.store.DeleteUser(r.Context(), id); err != nil {
			problem(w, 409, "delete_failed", err.Error())
			return
		}
		s.store.Audit(r.Context(), u.ID, "user.delete", id, r.RemoteAddr, "")
		w.WriteHeader(204)
	case r.Method == "PATCH" && strings.HasPrefix(path, "users/"):
		if !permit(w, u, "user.create") {
			return
		}
		id := strings.TrimPrefix(path, "users/")
		if id == u.ID {
			problem(w, 400, "self_update", "use another administrator to change your active account")
			return
		}
		var in struct {
			Disabled bool
			Role     string
		}
		if decode(r, &in) != nil {
			problem(w, 400, "invalid_request", "invalid JSON")
			return
		}
		if err := s.store.UpdateUser(r.Context(), id, in.Disabled, in.Role); err != nil {
			problem(w, 400, "user_update_failed", err.Error())
			return
		}
		s.store.Audit(r.Context(), u.ID, "user.update", id, r.RemoteAddr, in.Role)
		write(w, 200, map[string]string{"status": "updated"})
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
		cs, _ := s.store.ListCommands(r.Context(), 500)
		online, active := 0, 0
		for _, d := range ds {
			if !d.Revoked && s.deviceOnline(d.ID) {
				online++
			}
		}
		for _, c := range cs {
			if c.Status == "queued" || c.Status == "delivered" || c.Status == "accepted" {
				active++
			}
		}
		write(w, 200, map[string]any{"uptime_seconds": int(time.Since(s.started).Seconds()), "devices": len(ds), "users": len(us), "online_devices": online, "offline_devices": len(ds) - online, "active_commands": active, "adapter_name": s.adapterName, "adapter_address": s.adapterAddress, "adapter_ready": s.adapterName != "", "ping_interval_seconds": int(s.pingInterval().Seconds())})
	case r.Method == "GET" && path == "network/flows":
		if !permit(w, u, "network.flow.view") {
			return
		}
		s.networkFlows(w, r)
	case r.Method == "GET" && path == "commands":
		if !permit(w, u, "device.view") {
			return
		}
		var v []storage.Command
		var err error
		if auth.Role(u.Role) == auth.Member {
			v, err = s.store.ListCommandsByUser(r.Context(), u.ID, 200)
		} else {
			v, err = s.store.ListCommands(r.Context(), 200)
		}
		respond(w, v, err)
	case r.Method == "GET" && path == "audit":
		if !permit(w, u, "audit.view") {
			return
		}
		v, err := s.store.ListAudit(r.Context(), 200)
		respond(w, v, err)
	case r.Method == "POST" && (strings.HasSuffix(path, "/restart") || strings.HasSuffix(path, "/shutdown") || strings.HasSuffix(path, "/disconnect")):
		s.command(w, r, u, path)
	default:
		problem(w, 404, "not_found", "endpoint not found")
	}
}

type networkNode struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	IP       string `json:"ip"`
	Kind     string `json:"kind"`
	Online   bool   `json:"online"`
	Platform string `json:"platform,omitempty"`
}

type networkFlowResponse struct {
	routing.TrafficSnapshot
	Nodes []networkNode `json:"nodes"`
}

func (s *Server) networkFlows(w http.ResponseWriter, r *http.Request) {
	devices, err := s.store.ListDevices(r.Context())
	if err != nil {
		problem(w, 500, "network_flows_failed", "network flow data could not be loaded")
		return
	}
	serverIP := strings.SplitN(s.adapterAddress, "/", 2)[0]
	nodes := []networkNode{{ID: "server", Name: "TyxNet Server", IP: serverIP, Kind: "server", Online: true}}
	for _, device := range devices {
		if device.Revoked {
			continue
		}
		nodes = append(nodes, networkNode{ID: device.ID, Name: device.Name, IP: device.VirtualIP, Kind: "device", Online: s.deviceOnline(device.ID), Platform: device.OS + " / " + device.Arch})
	}
	write(w, 200, networkFlowResponse{TrafficSnapshot: s.traffic.Snapshot(), Nodes: nodes})
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
func (s *Server) updateUserPassword(w http.ResponseWriter, r *http.Request, actor storage.User, userID string) {
	var in struct {
		Password string `json:"password"`
	}
	if decode(r, &in) != nil {
		problem(w, 400, "invalid_request", "invalid JSON")
		return
	}
	passwordHash, err := auth.HashPassword(in.Password)
	if err != nil {
		problem(w, 400, "invalid_password", err.Error())
		return
	}
	if err = s.store.UpdateUserPassword(r.Context(), userID, passwordHash); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			problem(w, 404, "user_not_found", "user not found")
			return
		}
		problem(w, 500, "password_update_failed", "password could not be updated")
		return
	}
	s.store.Audit(r.Context(), actor.ID, "user.password.update", userID, r.RemoteAddr, "sessions revoked")
	write(w, 200, map[string]string{"status": "updated"})
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
	s.notifyCommand(parts[1])
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

func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	b, err := embeddedWeb.ReadFile("web/index.html")
	if err != nil {
		http.Error(w, "web UI unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(b)
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
