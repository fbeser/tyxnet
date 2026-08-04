package client

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/fbeser/tyxnet/internal/application"
	"github.com/fbeser/tyxnet/internal/config"
	"github.com/fbeser/tyxnet/internal/platform"
	"github.com/fbeser/tyxnet/internal/storage"
	"github.com/fbeser/tyxnet/internal/tunnel"
)

type State struct {
	mu             sync.RWMutex
	Connected      bool      `json:"connected"`
	DeviceID       string    `json:"device_id"`
	VirtualIP      string    `json:"virtual_ip"`
	VirtualNetwork string    `json:"virtual_network"`
	PingInterval   int       `json:"ping_interval_seconds"`
	AdapterName    string    `json:"adapter_name"`
	AdapterAddress string    `json:"adapter_address"`
	AdapterReady   bool      `json:"adapter_ready"`
	LastConnected  time.Time `json:"last_connected"`
	LastError      string    `json:"last_error"`
	Started        time.Time `json:"started"`
	Configured     bool      `json:"configured"`
}
type persisted struct {
	DeviceID, VirtualIP, VirtualNetwork string
	PrivateKey                          []byte
}
type ensureTunnelFunc func(context.Context, string, string, int) (tunnel.Device, error)
type Client struct {
	Config          config.Client
	State           *State
	HTTP            *http.Client
	key             ed25519.PrivateKey
	setupMu         sync.Mutex
	ready           chan struct{}
	readyOnce       sync.Once
	managementMu    sync.RWMutex
	managementToken string
	managementUser  storage.User
	managementNodes []storage.Device
	adapterMu       sync.Mutex
	adapter         tunnel.Device
	adapterAddress  string
	ensureTunnel    ensureTunnelFunc
	startupSpec     application.StartupSpec
	trayToken       string
	shutdown        func()
	startupEnabled  func(application.StartupSpec) (bool, error)
	setStartup      func(application.StartupSpec, bool) error
}

func New(c config.Client) *Client {
	return &Client{Config: c, State: &State{Started: time.Now()}, HTTP: &http.Client{Timeout: 30 * time.Second}, ready: make(chan struct{}), ensureTunnel: platform.EnsureTunnel, startupEnabled: application.StartupEnabled, setStartup: application.SetStartup}
}

func (c *Client) ConfigureApplication(spec application.StartupSpec, trayToken string, shutdown func()) {
	c.startupSpec = spec
	c.trayToken = trayToken
	c.shutdown = shutdown
}
func (c *Client) Join(ctx context.Context, token string) error {
	if strings.TrimSpace(token) == "" {
		return errors.New("enrollment token is required")
	}
	if _, err := os.Stat(filepath.Join(c.Config.StateDir, "identity.json")); err == nil {
		return errors.New("client is already configured")
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("check identity: %w", err)
	}
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	in := map[string]any{"Token": token, "Name": c.Config.Name, "OS": runtime.GOOS, "Arch": runtime.GOARCH, "Version": "dev", "public_key": priv.Public().(ed25519.PublicKey)}
	var d storage.Device
	if err = c.request(ctx, "POST", "/api/v1/enroll", in, &d); err != nil {
		return err
	}
	if err = os.MkdirAll(c.Config.StateDir, 0700); err != nil {
		return err
	}
	p := persisted{DeviceID: d.ID, VirtualIP: d.VirtualIP, PrivateKey: priv}
	b, _ := json.Marshal(p)
	if err = os.WriteFile(filepath.Join(c.Config.StateDir, "identity.json"), b, 0600); err != nil {
		return err
	}
	c.key = priv
	c.State.mu.Lock()
	c.State.DeviceID = d.ID
	c.State.VirtualIP = d.VirtualIP
	c.State.Configured = true
	c.State.mu.Unlock()
	c.readyOnce.Do(func() { close(c.ready) })
	return nil
}
func (c *Client) load() error {
	b, err := os.ReadFile(filepath.Join(c.Config.StateDir, "identity.json"))
	if err != nil {
		return fmt.Errorf("load identity: %w", err)
	}
	var p persisted
	if err = json.Unmarshal(b, &p); err != nil {
		return err
	}
	if len(p.PrivateKey) != ed25519.PrivateKeySize {
		return errors.New("invalid private key")
	}
	c.key = p.PrivateKey
	c.State.mu.Lock()
	c.State.DeviceID = p.DeviceID
	c.State.VirtualIP = p.VirtualIP
	c.State.VirtualNetwork = p.VirtualNetwork
	c.State.Configured = true
	c.State.mu.Unlock()
	return nil
}
func (c *Client) Run(ctx context.Context) error {
	defer c.closeAdapter()
	if err := c.load(); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		case <-c.ready:
		}
	}
	backoff := []time.Duration{time.Second, 2 * time.Second, 5 * time.Second, 10 * time.Second, 30 * time.Second, time.Minute}
	for i := 0; ; i++ {
		if err := c.connect(ctx); err != nil {
			c.State.mu.Lock()
			c.State.Connected = false
			c.State.LastError = err.Error()
			c.State.mu.Unlock()
		}
		if ctx.Err() != nil {
			return nil
		}
		d := backoff[min(i, len(backoff)-1)]
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(d):
		}
	}
}
func (c *Client) connect(ctx context.Context) error {
	var challenge struct {
		Challenge string `json:"challenge"`
	}
	if err := c.request(ctx, "POST", "/control/v1/challenge", map[string]string{"device_id": c.State.DeviceID}, &challenge); err != nil {
		return err
	}
	nonce, err := base64.RawStdEncoding.DecodeString(challenge.Challenge)
	if err != nil {
		return err
	}
	sig := ed25519.Sign(c.key, nonce)
	body, _ := json.Marshal(map[string]string{"DeviceID": c.State.DeviceID, "Signature": base64.RawStdEncoding.EncodeToString(sig)})
	req, err := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(c.Config.ServerURL, "/")+"/control/v1/connect", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	streamClient := *c.HTTP
	streamClient.Timeout = 0
	resp, err := streamClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("control connect: %s: %s", resp.Status, string(b))
	}
	c.State.mu.Lock()
	c.State.Connected = true
	c.State.LastConnected = time.Now()
	c.State.LastError = ""
	c.State.mu.Unlock()
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		if ctx.Err() != nil {
			return nil
		}
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			var update struct {
				VirtualIP           string `json:"virtual_ip"`
				VirtualNetwork      string `json:"virtual_network"`
				PingIntervalSeconds int    `json:"ping_interval_seconds"`
			}
			if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &update) == nil {
				if err := c.applyServerState(ctx, update.VirtualIP, update.VirtualNetwork, update.PingIntervalSeconds); err != nil {
					return err
				}
			}
		}
	}
	return scanner.Err()
}

func (c *Client) applyServerState(ctx context.Context, virtualIP, virtualNetwork string, pingIntervalSeconds int) error {
	c.State.mu.Lock()
	changedIP := virtualIP != "" && virtualIP != c.State.VirtualIP
	changedNetwork := virtualNetwork != "" && virtualNetwork != c.State.VirtualNetwork
	if virtualIP != "" {
		c.State.VirtualIP = virtualIP
	}
	if virtualNetwork != "" {
		c.State.VirtualNetwork = virtualNetwork
	}
	if pingIntervalSeconds > 0 {
		c.State.PingInterval = pingIntervalSeconds
	}
	deviceID := c.State.DeviceID
	currentIP := c.State.VirtualIP
	currentNetwork := c.State.VirtualNetwork
	c.State.mu.Unlock()
	if changedIP || changedNetwork {
		p := persisted{DeviceID: deviceID, VirtualIP: currentIP, VirtualNetwork: currentNetwork, PrivateKey: c.key}
		b, err := json.Marshal(p)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(c.Config.StateDir, "identity.json"), b, 0600); err != nil {
			return fmt.Errorf("save assigned virtual IP: %w", err)
		}
	}
	return c.ensureClientAdapter(ctx, currentIP, currentNetwork)
}

func (c *Client) ensureClientAdapter(ctx context.Context, virtualIP, virtualNetwork string) error {
	if !c.Config.TunnelEnabled {
		return nil
	}
	if virtualIP == "" || virtualNetwork == "" {
		return nil
	}
	ip := net.ParseIP(virtualIP)
	_, network, err := net.ParseCIDR(virtualNetwork)
	if err != nil || ip == nil || ip.To4() == nil || !network.Contains(ip) {
		return errors.New("server returned an invalid client virtual network assignment")
	}
	prefix, bits := network.Mask.Size()
	if bits != 32 {
		return errors.New("client virtual network must be IPv4")
	}
	address := fmt.Sprintf("%s/%d", ip.String(), prefix)
	c.adapterMu.Lock()
	defer c.adapterMu.Unlock()
	if c.adapter != nil && c.adapterAddress == address {
		return nil
	}
	if c.adapter != nil {
		_ = c.adapter.Close()
		c.adapter = nil
		c.adapterAddress = ""
	}
	name := c.Config.TunnelName
	if name == "" {
		name = clientAdapterName(c.Config.ServerURL)
	}
	device, err := c.ensureTunnel(ctx, name, address, c.Config.TunnelMTU)
	if err != nil {
		return fmt.Errorf("ensure client virtual adapter: %w", err)
	}
	c.adapter = device
	c.adapterAddress = address
	c.State.mu.Lock()
	c.State.AdapterName = device.Name()
	c.State.AdapterAddress = address
	c.State.AdapterReady = true
	c.State.mu.Unlock()
	return nil
}

func (c *Client) closeAdapter() {
	c.adapterMu.Lock()
	defer c.adapterMu.Unlock()
	if c.adapter != nil {
		_ = c.adapter.Close()
		c.adapter = nil
	}
	c.State.mu.Lock()
	c.State.AdapterReady = false
	c.State.mu.Unlock()
}

func clientAdapterName(serverURL string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimRight(serverURL, "/"))))
	return "TyxC-" + hex.EncodeToString(sum[:4])
}
func (c *Client) request(ctx context.Context, method, path string, in, out any) error {
	return c.requestWithToken(ctx, method, path, "", in, out)
}
func (c *Client) requestWithToken(ctx context.Context, method, path, token string, in, out any) error {
	b, _ := json.Marshal(in)
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.Config.ServerURL, "/")+path, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		x, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("server: %s: %s", resp.Status, string(x))
	}
	if out == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

//go:embed web/*
var embeddedWeb embed.FS

func (c *Client) LocalHandler(configPath string) http.Handler {
	m := http.NewServeMux()
	m.HandleFunc("GET /api/status", func(w http.ResponseWriter, _ *http.Request) {
		c.State.mu.RLock()
		defer c.State.mu.RUnlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(c.State)
	})
	m.HandleFunc("POST /api/setup", func(w http.ResponseWriter, r *http.Request) {
		c.setupMu.Lock()
		defer c.setupMu.Unlock()
		origin := r.Header.Get("Origin")
		if origin != "" && origin != "http://"+r.Host && origin != "https://"+r.Host {
			http.Error(w, "Cross-origin setup is not allowed", http.StatusForbidden)
			return
		}
		identityPath := filepath.Join(c.Config.StateDir, "identity.json")
		if _, err := os.Stat(identityPath); err == nil {
			http.Error(w, "Client is already configured", http.StatusConflict)
			return
		} else if !errors.Is(err, os.ErrNotExist) {
			http.Error(w, "Unable to inspect client identity", http.StatusInternalServerError)
			return
		}
		var in struct {
			Server string `json:"server"`
			Token  string `json:"token"`
			Name   string `json:"name"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, "Invalid setup request", http.StatusBadRequest)
			return
		}
		cfg := c.Config
		cfg.ServerURL = strings.TrimSpace(in.Server)
		cfg.Name = strings.TrimSpace(in.Name)
		if err := cfg.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		c.Config = cfg
		if err := config.SaveClient(configPath, cfg); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := c.Join(r.Context(), in.Token); err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"configured": true, "device_id": c.State.DeviceID, "virtual_ip": c.State.VirtualIP})
	})
	m.HandleFunc("POST /api/management/login", c.managementLogin)
	m.HandleFunc("POST /api/management/logout", c.managementLogout)
	m.HandleFunc("GET /api/management/me", c.managementMe)
	m.HandleFunc("GET /api/management/devices", c.managementDevices)
	m.HandleFunc("POST /api/management/devices/{id}/{action}", c.managementCommand)
	m.HandleFunc("GET /api/management/startup", c.managementStartup)
	m.HandleFunc("PATCH /api/management/startup", c.managementStartup)
	m.HandleFunc("GET /api/tray", c.traySnapshot)
	m.HandleFunc("POST /api/tray/devices/{id}/{action}", c.trayCommand)
	m.HandleFunc("POST /api/tray/startup", c.trayStartup)
	m.HandleFunc("POST /api/tray/quit", c.trayQuit)
	m.HandleFunc("GET /", func(w http.ResponseWriter, _ *http.Request) {
		b, err := embeddedWeb.ReadFile("web/index.html")
		if err != nil {
			http.Error(w, "UI unavailable", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(b)
	})
	assets, _ := fs.Sub(embeddedWeb, "web")
	m.Handle("GET /ui/", http.StripPrefix("/ui/", http.FileServer(http.FS(assets))))
	return securityHeaders(m)
}

type managementLoginResponse struct {
	AccessToken string       `json:"access_token"`
	ExpiresIn   int          `json:"expires_in"`
	User        storage.User `json:"user"`
}

func (c *Client) managementLogin(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "Invalid login request", http.StatusBadRequest)
		return
	}
	var result managementLoginResponse
	if err := c.request(r.Context(), http.MethodPost, "/api/v1/auth/login", in, &result); err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	c.managementMu.Lock()
	c.managementToken = result.AccessToken
	c.managementUser = result.User
	c.managementNodes = nil
	c.managementMu.Unlock()
	writeClientJSON(w, http.StatusOK, result)
}

func (c *Client) managementLogout(w http.ResponseWriter, r *http.Request) {
	token := incomingBearer(r)
	if token != "" {
		_ = c.requestWithToken(r.Context(), http.MethodPost, "/api/v1/auth/logout", token, nil, nil)
	}
	c.managementMu.Lock()
	c.managementToken = ""
	c.managementUser = storage.User{}
	c.managementNodes = nil
	c.managementMu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func (c *Client) managementMe(w http.ResponseWriter, r *http.Request) {
	token := incomingBearer(r)
	var user storage.User
	if token == "" || c.requestWithToken(r.Context(), http.MethodGet, "/api/v1/auth/me", token, nil, &user) != nil {
		http.Error(w, "Valid server login required", http.StatusUnauthorized)
		return
	}
	c.managementMu.Lock()
	c.managementToken = token
	c.managementUser = user
	c.managementMu.Unlock()
	writeClientJSON(w, http.StatusOK, user)
}

func (c *Client) managementDevices(w http.ResponseWriter, r *http.Request) {
	token := incomingBearer(r)
	var devices []storage.Device
	if token == "" || c.requestWithToken(r.Context(), http.MethodGet, "/api/v1/devices", token, nil, &devices) != nil {
		http.Error(w, "Device list is not permitted for this session", http.StatusUnauthorized)
		return
	}
	c.managementMu.Lock()
	c.managementToken = token
	c.managementNodes = append([]storage.Device(nil), devices...)
	c.managementMu.Unlock()
	writeClientJSON(w, http.StatusOK, devices)
}

func (c *Client) managementCommand(w http.ResponseWriter, r *http.Request) {
	action := r.PathValue("action")
	if action != "disconnect" && action != "restart" && action != "shutdown" {
		http.Error(w, "Unsupported device action", http.StatusBadRequest)
		return
	}
	token := incomingBearer(r)
	var result storage.Command
	path := "/api/v1/devices/" + r.PathValue("id") + "/" + action
	if token == "" || c.requestWithToken(r.Context(), http.MethodPost, path, token, nil, &result) != nil {
		http.Error(w, "Device action is not permitted for this session", http.StatusForbidden)
		return
	}
	writeClientJSON(w, http.StatusAccepted, result)
}

func (c *Client) managementStartup(w http.ResponseWriter, r *http.Request) {
	token := incomingBearer(r)
	var user storage.User
	if token == "" || c.requestWithToken(r.Context(), http.MethodGet, "/api/v1/auth/me", token, nil, &user) != nil {
		http.Error(w, "Valid server login required", http.StatusUnauthorized)
		return
	}
	if user.Role != "admin" {
		http.Error(w, "Administrator role required", http.StatusForbidden)
		return
	}
	if r.Method == http.MethodGet {
		enabled, err := c.startupEnabled(c.startupSpec)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeClientJSON(w, http.StatusOK, map[string]bool{"enabled": enabled})
		return
	}
	c.updateStartup(w, r)
}

func incomingBearer(r *http.Request) string {
	parts := strings.Fields(r.Header.Get("Authorization"))
	if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
		return parts[1]
	}
	return ""
}

func writeClientJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func (c *Client) traySnapshot(w http.ResponseWriter, r *http.Request) {
	if !c.trayAllowed(r) {
		http.Error(w, "Local tray authentication required", http.StatusForbidden)
		return
	}
	c.managementMu.RLock()
	token := c.managementToken
	user := c.managementUser
	c.managementMu.RUnlock()
	devices := []storage.Device{}
	if token != "" {
		if err := c.requestWithToken(r.Context(), http.MethodGet, "/api/v1/devices", token, nil, &devices); err != nil {
			c.managementMu.Lock()
			c.managementToken = ""
			c.managementUser = storage.User{}
			c.managementNodes = nil
			c.managementMu.Unlock()
			user = storage.User{}
			devices = nil
		} else {
			c.managementMu.Lock()
			c.managementNodes = append([]storage.Device(nil), devices...)
			c.managementMu.Unlock()
		}
	}
	c.State.mu.RLock()
	local := map[string]any{"configured": c.State.Configured, "connected": c.State.Connected, "virtual_ip": c.State.VirtualIP, "last_error": c.State.LastError}
	c.State.mu.RUnlock()
	startupEnabled, _ := c.startupEnabled(c.startupSpec)
	writeClientJSON(w, http.StatusOK, map[string]any{"local": local, "user": user, "devices": devices, "startup_enabled": startupEnabled})
}

func (c *Client) trayCommand(w http.ResponseWriter, r *http.Request) {
	if !c.trayAllowed(r) {
		http.Error(w, "Local tray authentication required", http.StatusForbidden)
		return
	}
	action := r.PathValue("action")
	if action != "disconnect" && action != "restart" && action != "shutdown" {
		http.Error(w, "Unsupported device action", http.StatusBadRequest)
		return
	}
	c.managementMu.RLock()
	token := c.managementToken
	c.managementMu.RUnlock()
	var result storage.Command
	path := "/api/v1/devices/" + r.PathValue("id") + "/" + action
	if token == "" || c.requestWithToken(r.Context(), http.MethodPost, path, token, nil, &result) != nil {
		http.Error(w, "Sign in through the web console with a permitted role", http.StatusForbidden)
		return
	}
	writeClientJSON(w, http.StatusAccepted, result)
}

func (c *Client) trayAllowed(r *http.Request) bool {
	provided := r.Header.Get("X-TyxNet-Tray-Token")
	return clientRemoteIsLoopback(r.RemoteAddr) && c.trayToken != "" && subtle.ConstantTimeCompare([]byte(provided), []byte(c.trayToken)) == 1
}

func (c *Client) trayStartup(w http.ResponseWriter, r *http.Request) {
	if !c.trayAllowed(r) {
		http.Error(w, "Local tray authentication required", http.StatusForbidden)
		return
	}
	c.updateStartup(w, r)
}

func (c *Client) updateStartup(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Enabled bool `json:"enabled"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "Invalid startup request", http.StatusBadRequest)
		return
	}
	if err := c.setStartup(c.startupSpec, in.Enabled); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeClientJSON(w, http.StatusOK, map[string]bool{"enabled": in.Enabled})
}

func (c *Client) trayQuit(w http.ResponseWriter, r *http.Request) {
	if !c.trayAllowed(r) {
		http.Error(w, "Local tray authentication required", http.StatusForbidden)
		return
	}
	w.WriteHeader(http.StatusAccepted)
	if c.shutdown != nil {
		go func() {
			time.Sleep(100 * time.Millisecond)
			c.shutdown()
		}()
	}
}

func clientRemoteIsLoopback(remote string) bool {
	host, _, err := net.SplitHostPort(remote)
	ip := net.ParseIP(host)
	return err == nil && ip != nil && ip.IsLoopback()
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if r.Method != http.MethodGet && r.Method != http.MethodHead && origin != "" && origin != "http://"+r.Host && origin != "https://"+r.Host {
			http.Error(w, "Cross-origin request is not allowed", http.StatusForbidden)
			return
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self'; script-src 'self'")
		next.ServeHTTP(w, r)
	})
}
