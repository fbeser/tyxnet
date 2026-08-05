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
	"github.com/fbeser/tyxnet/internal/buildinfo"
	"github.com/fbeser/tyxnet/internal/commands"
	"github.com/fbeser/tyxnet/internal/config"
	"github.com/fbeser/tyxnet/internal/dataplane"
	"github.com/fbeser/tyxnet/internal/platform"
	"github.com/fbeser/tyxnet/internal/storage"
	"github.com/fbeser/tyxnet/internal/tunnel"
	"github.com/fbeser/tyxnet/pkg/protocol"
)

var errReconnectRequested = errors.New("server requested reconnect")

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
	DataPlaneReady bool      `json:"data_plane_ready"`
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
	configMu        sync.RWMutex
	setupMu         sync.Mutex
	configurationCh chan struct{}
	connectionMu    sync.Mutex
	connectionStop  context.CancelFunc
	managementMu    sync.RWMutex
	managementToken string
	managementUser  storage.User
	managementNodes []storage.Device
	adapterMu       sync.Mutex
	adapter         tunnel.Device
	adapterAddress  string
	dataPlaneMu     sync.Mutex
	dataPlane       *dataplane.Client
	ensureTunnel    ensureTunnelFunc
	startupSpec     application.StartupSpec
	trayToken       string
	shutdown        func()
	startupEnabled  func(application.StartupSpec) (bool, error)
	setStartup      func(application.StartupSpec, bool) error
	executeCommand  func(context.Context, string) error
	commandMu       sync.Mutex
	handledCommands map[string]time.Time
}

func New(c config.Client) *Client {
	return &Client{Config: c, State: &State{Started: time.Now()}, HTTP: &http.Client{Timeout: 30 * time.Second}, configurationCh: make(chan struct{}, 1), ensureTunnel: platform.EnsureTunnel, startupEnabled: application.StartupEnabled, setStartup: application.SetStartup, executeCommand: commands.ExecuteSystem, handledCommands: map[string]time.Time{}}
}

func (c *Client) configuration() config.Client {
	c.configMu.RLock()
	defer c.configMu.RUnlock()
	return c.Config
}

func (c *Client) setConfiguration(cfg config.Client) {
	c.configMu.Lock()
	c.Config = cfg
	c.configMu.Unlock()
}

func (c *Client) notifyConfigurationChanged() {
	select {
	case c.configurationCh <- struct{}{}:
	default:
	}
}

func (c *Client) cancelConnection() {
	c.connectionMu.Lock()
	if c.connectionStop != nil {
		c.connectionStop()
	}
	c.connectionMu.Unlock()
}

func (c *Client) ConfigureApplication(spec application.StartupSpec, trayToken string, shutdown func()) {
	c.startupSpec = spec
	c.trayToken = trayToken
	c.shutdown = shutdown
}
func (c *Client) Join(ctx context.Context, token string) error {
	cfg := c.configuration()
	if strings.TrimSpace(token) == "" {
		return errors.New("enrollment token is required")
	}
	if _, err := os.Stat(filepath.Join(cfg.StateDir, "identity.json")); err == nil {
		return errors.New("client is already configured")
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("check identity: %w", err)
	}
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	in := map[string]any{"Token": token, "Name": cfg.Name, "OS": runtime.GOOS, "Arch": runtime.GOARCH, "Version": buildinfo.Version, "public_key": priv.Public().(ed25519.PublicKey)}
	var d storage.Device
	if err = c.request(ctx, "POST", "/api/v1/enroll", in, &d); err != nil {
		return err
	}
	if err = os.MkdirAll(cfg.StateDir, 0700); err != nil {
		return err
	}
	p := persisted{DeviceID: d.ID, VirtualIP: d.VirtualIP, PrivateKey: priv}
	b, _ := json.Marshal(p)
	if err = os.WriteFile(filepath.Join(cfg.StateDir, "identity.json"), b, 0600); err != nil {
		return err
	}
	c.key = priv
	c.State.mu.Lock()
	c.State.DeviceID = d.ID
	c.State.VirtualIP = d.VirtualIP
	c.State.Configured = true
	c.State.mu.Unlock()
	c.notifyConfigurationChanged()
	return nil
}
func (c *Client) load() error {
	cfg := c.configuration()
	b, err := os.ReadFile(filepath.Join(cfg.StateDir, "identity.json"))
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
	}
	backoff := []time.Duration{time.Second, 2 * time.Second, 5 * time.Second, 10 * time.Second, 30 * time.Second, time.Minute}
	for i := 0; ; i++ {
		c.State.mu.RLock()
		configured := c.State.Configured
		c.State.mu.RUnlock()
		if !configured {
			c.closeAdapter()
			i = 0
			select {
			case <-ctx.Done():
				return nil
			case <-c.configurationCh:
			}
			continue
		}
		connectionContext, stopConnection := context.WithCancel(ctx)
		c.connectionMu.Lock()
		c.connectionStop = stopConnection
		c.connectionMu.Unlock()
		err := c.connect(connectionContext)
		stopConnection()
		c.connectionMu.Lock()
		c.connectionStop = nil
		c.connectionMu.Unlock()
		c.State.mu.RLock()
		configured = c.State.Configured
		c.State.mu.RUnlock()
		if !configured {
			continue
		}
		if err != nil {
			c.State.mu.Lock()
			c.State.Connected = false
			if errors.Is(err, errReconnectRequested) {
				c.State.LastError = ""
				i = 0
			} else {
				c.State.LastError = err.Error()
			}
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
	cfg := c.configuration()
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
	req, err := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(cfg.ServerURL, "/")+"/control/v1/connect", bytes.NewReader(body))
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
	eventType := ""
	for scanner.Scan() {
		if ctx.Err() != nil {
			return nil
		}
		line := scanner.Text()
		if strings.HasPrefix(line, "event: ") {
			eventType = strings.TrimPrefix(line, "event: ")
			continue
		}
		if strings.HasPrefix(line, "data: ") {
			data := []byte(strings.TrimPrefix(line, "data: "))
			if eventType == "command" {
				var command protocol.ControlCommand
				if err := json.Unmarshal(data, &command); err != nil {
					return fmt.Errorf("decode control command: %w", err)
				}
				if err := c.handleControlCommand(ctx, command); err != nil {
					return err
				}
				continue
			}
			var update struct {
				VirtualIP           string               `json:"virtual_ip"`
				VirtualNetwork      string               `json:"virtual_network"`
				PingIntervalSeconds int                  `json:"ping_interval_seconds"`
				DataPlane           *dataplane.Bootstrap `json:"data_plane"`
			}
			if json.Unmarshal(data, &update) == nil {
				if err := c.applyServerState(ctx, update.VirtualIP, update.VirtualNetwork, update.PingIntervalSeconds); err != nil {
					return err
				}
				if update.DataPlane != nil {
					if err := c.configureDataPlane(*update.DataPlane); err != nil {
						return fmt.Errorf("configure data plane: %w", err)
					}
				}
			}
		}
	}
	return scanner.Err()
}

func (c *Client) handleControlCommand(ctx context.Context, command protocol.ControlCommand) error {
	if command.ProtocolVersion != protocol.Version || command.ID == "" || time.Now().After(command.ExpiresAt) {
		return errors.New("server sent an invalid or expired command")
	}
	if command.Type != "system.restart" && command.Type != "system.shutdown" && command.Type != "client.reconnect" {
		return errors.New("server sent a command outside the executable allowlist")
	}
	c.commandMu.Lock()
	for commandID, expiresAt := range c.handledCommands {
		if time.Now().After(expiresAt) {
			delete(c.handledCommands, commandID)
		}
	}
	_, alreadyHandled := c.handledCommands[command.ID]
	c.commandMu.Unlock()
	if alreadyHandled {
		return nil
	}
	if err := c.reportCommandResult(ctx, command.ID, "accepted", "", ""); err != nil {
		return fmt.Errorf("accept command: %w", err)
	}
	c.commandMu.Lock()
	c.handledCommands[command.ID] = command.ExpiresAt
	c.commandMu.Unlock()
	if command.Type == "client.reconnect" {
		if err := c.reportCommandResult(ctx, command.ID, "succeeded", "control connection reconnecting", ""); err != nil {
			return fmt.Errorf("report reconnect: %w", err)
		}
		return errReconnectRequested
	}
	executionContext, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	err := c.executeCommand(executionContext, command.Type)
	cancel()
	if err != nil {
		if reportErr := c.reportCommandResult(ctx, command.ID, "failed", "", "system action failed"); reportErr != nil {
			return fmt.Errorf("execute command: %v; report failure: %w", err, reportErr)
		}
		return nil
	}
	if err := c.reportCommandResult(ctx, command.ID, "succeeded", "system action scheduled", ""); err != nil {
		return fmt.Errorf("report command success: %w", err)
	}
	return nil
}

func (c *Client) reportCommandResult(ctx context.Context, commandID, status, resultText, errorText string) error {
	for attempt := 0; attempt < 3; attempt++ {
		var challenge struct {
			Challenge string `json:"challenge"`
		}
		if err := c.request(ctx, http.MethodPost, "/control/v1/challenge", map[string]string{"device_id": c.State.DeviceID}, &challenge); err != nil {
			continue
		}
		nonce, err := base64.RawStdEncoding.DecodeString(challenge.Challenge)
		if err != nil {
			continue
		}
		payload, err := protocol.CommandResultSigningPayload(nonce, c.State.DeviceID, commandID, status, resultText, errorText)
		if err != nil {
			return err
		}
		result := protocol.CommandResult{DeviceID: c.State.DeviceID, Status: status, Result: resultText, Error: errorText, Signature: base64.RawStdEncoding.EncodeToString(ed25519.Sign(c.key, payload))}
		if err = c.request(ctx, http.MethodPost, "/control/v1/commands/"+commandID+"/result", result, nil); err == nil {
			return nil
		}
	}
	return errors.New("command result could not be authenticated with the server")
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
		cfg := c.configuration()
		p := persisted{DeviceID: deviceID, VirtualIP: currentIP, VirtualNetwork: currentNetwork, PrivateKey: c.key}
		b, err := json.Marshal(p)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(cfg.StateDir, "identity.json"), b, 0600); err != nil {
			return fmt.Errorf("save assigned virtual IP: %w", err)
		}
	}
	return c.ensureClientAdapter(ctx, currentIP, currentNetwork)
}

func (c *Client) ensureClientAdapter(ctx context.Context, virtualIP, virtualNetwork string) error {
	cfg := c.configuration()
	if !cfg.TunnelEnabled {
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
	if c.adapter != nil && c.adapterAddress == address {
		c.adapterMu.Unlock()
		return nil
	}
	hasAdapter := c.adapter != nil
	c.adapterMu.Unlock()
	if hasAdapter {
		c.closeDataPlane()
	}
	c.adapterMu.Lock()
	defer c.adapterMu.Unlock()
	if c.adapter != nil {
		_ = c.adapter.Close()
		c.adapter = nil
		c.adapterAddress = ""
	}
	name := cfg.TunnelName
	if name == "" {
		name = clientAdapterName(cfg.ServerURL)
	}
	device, err := c.ensureTunnel(ctx, name, address, cfg.TunnelMTU)
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
	c.closeDataPlane()
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

func (c *Client) closeDataPlane() {
	c.dataPlaneMu.Lock()
	if c.dataPlane != nil {
		_ = c.dataPlane.Close()
		c.dataPlane = nil
	}
	c.dataPlaneMu.Unlock()
	c.State.mu.Lock()
	c.State.DataPlaneReady = false
	c.State.mu.Unlock()
}

func (c *Client) configureDataPlane(bootstrap dataplane.Bootstrap) error {
	cfg := c.configuration()
	c.adapterMu.Lock()
	adapter := c.adapter
	c.adapterMu.Unlock()
	if adapter == nil {
		return errors.New("client adapter is unavailable")
	}
	c.State.mu.RLock()
	assignedIP := net.ParseIP(c.State.VirtualIP)
	deviceID := c.State.DeviceID
	c.State.mu.RUnlock()
	c.dataPlaneMu.Lock()
	defer c.dataPlaneMu.Unlock()
	if c.dataPlane == nil {
		dataPlane, err := dataplane.NewClient(adapter, assignedIP, deviceID, cfg.TunnelMTU)
		if err != nil {
			return err
		}
		c.dataPlane = dataPlane
	}
	if err := c.dataPlane.Configure(cfg.ServerURL, cfg.TunnelEndpoint, bootstrap); err != nil {
		return err
	}
	c.State.mu.Lock()
	c.State.DataPlaneReady = true
	c.State.mu.Unlock()
	return nil
}

func clientAdapterName(serverURL string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimRight(serverURL, "/"))))
	return "TyxC-" + hex.EncodeToString(sum[:4])
}
func (c *Client) request(ctx context.Context, method, path string, in, out any) error {
	return c.requestWithToken(ctx, method, path, "", in, out)
}
func (c *Client) requestWithToken(ctx context.Context, method, path, token string, in, out any) error {
	cfg := c.configuration()
	b, _ := json.Marshal(in)
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(cfg.ServerURL, "/")+path, bytes.NewReader(b))
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

func clientSameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	return origin == "" || origin == "http://"+r.Host || origin == "https://"+r.Host
}

func (c *Client) forgetServer(configPath string) error {
	cfg := c.configuration()
	identityPath := filepath.Join(cfg.StateDir, "identity.json")
	if err := os.Remove(identityPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove client identity: %w", err)
	}
	cfg.ServerURL = ""
	cfg.TunnelEndpoint = ""
	cfg.Name = ""
	c.setConfiguration(cfg)
	c.cancelConnection()
	c.closeAdapter()
	c.managementMu.Lock()
	c.managementToken = ""
	c.managementUser = storage.User{}
	c.managementNodes = nil
	c.managementMu.Unlock()
	c.State.mu.Lock()
	c.State.Connected = false
	c.State.DeviceID = ""
	c.State.VirtualIP = ""
	c.State.VirtualNetwork = ""
	c.State.PingInterval = 0
	c.State.AdapterName = ""
	c.State.AdapterAddress = ""
	c.State.AdapterReady = false
	c.State.DataPlaneReady = false
	c.State.LastConnected = time.Time{}
	c.State.LastError = ""
	c.State.Configured = false
	c.State.mu.Unlock()
	if err := config.SaveClient(configPath, cfg); err != nil {
		return fmt.Errorf("save unconfigured client: %w", err)
	}
	return nil
}

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
		if !clientSameOrigin(r) {
			http.Error(w, "Cross-origin setup is not allowed", http.StatusForbidden)
			return
		}
		cfg := c.configuration()
		identityPath := filepath.Join(cfg.StateDir, "identity.json")
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
		cfg.ServerURL = strings.TrimSpace(in.Server)
		cfg.Name = strings.TrimSpace(in.Name)
		if err := cfg.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		c.setConfiguration(cfg)
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
	m.HandleFunc("DELETE /api/configuration", func(w http.ResponseWriter, r *http.Request) {
		if !clientRemoteIsLoopback(r.RemoteAddr) {
			http.Error(w, "Client reset is restricted to this device", http.StatusForbidden)
			return
		}
		if !clientSameOrigin(r) {
			http.Error(w, "Cross-origin reset is not allowed", http.StatusForbidden)
			return
		}
		c.setupMu.Lock()
		defer c.setupMu.Unlock()
		c.State.mu.RLock()
		configured := c.State.Configured
		c.State.mu.RUnlock()
		if !configured {
			http.Error(w, "Client is not configured", http.StatusConflict)
			return
		}
		if err := c.forgetServer(configPath); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
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
		Remember bool   `json:"remember"`
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
