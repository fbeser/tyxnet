package client

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fbeser/tyxnet/internal/application"
	"github.com/fbeser/tyxnet/internal/config"
	"github.com/fbeser/tyxnet/internal/tunnel"
)

func TestApplyServerStatePersistsStaticIP(t *testing.T) {
	dir := t.TempDir()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	c := New(config.Client{StateDir: dir})
	c.key = privateKey
	c.State.DeviceID = "dev_test"
	c.State.VirtualIP = "10.90.0.2"
	if err := c.applyServerState(context.Background(), "10.90.0.42", "10.90.0.0/24", 60); err != nil {
		t.Fatal(err)
	}
	var saved persisted
	b, err := os.ReadFile(filepath.Join(dir, "identity.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.VirtualIP != "10.90.0.42" || c.State.PingInterval != 60 {
		t.Fatalf("server state was not applied: %+v %+v", saved, c.State)
	}
}

type fakeTunnel struct {
	name   string
	closed bool
}

func (f *fakeTunnel) Name() string                { return f.name }
func (f *fakeTunnel) Close() error                { f.closed = true; return nil }
func (f *fakeTunnel) Read([]byte) (int, error)    { return 0, nil }
func (f *fakeTunnel) Write(p []byte) (int, error) { return len(p), nil }

func TestClientCreatesServerSpecificAdapter(t *testing.T) {
	cfg := config.DefaultClient()
	cfg.ServerURL = "https://server-a.example:8443"
	cfg.StateDir = t.TempDir()
	c := New(cfg)
	c.State.DeviceID = "dev_test"
	c.State.VirtualIP = "10.90.0.2"
	var gotName, gotAddress string
	c.ensureTunnel = func(_ context.Context, name, address string, mtu int) (tunnel.Device, error) {
		gotName, gotAddress = name, address
		if mtu != 1280 {
			t.Fatalf("unexpected MTU: %d", mtu)
		}
		return &fakeTunnel{name: name}, nil
	}
	if err := c.applyServerState(context.Background(), "10.90.0.2", "10.90.0.0/24", 25); err != nil {
		t.Fatal(err)
	}
	if gotName != clientAdapterName(cfg.ServerURL) || gotAddress != "10.90.0.2/24" || !c.State.AdapterReady {
		t.Fatalf("unexpected adapter state: name=%s address=%s state=%+v", gotName, gotAddress, c.State)
	}
	if clientAdapterName(cfg.ServerURL) == clientAdapterName("https://server-b.example:8443") {
		t.Fatal("different servers received the same adapter name")
	}
	c.closeAdapter()
}

func TestWebSetupEnrollsAndPersistsClient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/enroll" {
			t.Fatalf("unexpected enrollment path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ID":"dev_test","VirtualIP":"10.90.0.2"}`))
	}))
	defer server.Close()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "client.yaml")
	cfg := config.DefaultClient()
	cfg.ServerURL = "https://placeholder.example"
	cfg.Name = "placeholder"
	cfg.StateDir = filepath.Join(dir, "state")
	client := New(cfg)
	h := client.LocalHandler(configPath)

	body := bytes.NewBufferString(`{"server":"` + server.URL + `","name":"test-pc","token":"TYX-TEST"}`)
	r := httptest.NewRequest(http.MethodPost, "/api/setup", body)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("setup: %d %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(filepath.Join(cfg.StateDir, "identity.json")); err != nil {
		t.Fatalf("identity not saved: %v", err)
	}
	saved, err := config.LoadClient(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if saved.ServerURL != server.URL || saved.Name != "test-pc" {
		t.Fatalf("unexpected saved config: %+v", saved)
	}

	r = httptest.NewRequest(http.MethodGet, "/api/status", nil)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if !bytes.Contains(w.Body.Bytes(), []byte(`"configured":true`)) {
		t.Fatalf("status: %s", w.Body.String())
	}
}

func TestManagementProxyAndLoopbackTrayAPI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/auth/login":
			_, _ = w.Write([]byte(`{"access_token":"session-test","expires_in":900,"user":{"ID":"usr_test","Username":"admin","Role":"admin"}}`))
		case "/api/v1/auth/me":
			if r.Header.Get("Authorization") != "Bearer session-test" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(`{"ID":"usr_test","Username":"admin","Role":"admin"}`))
		case "/api/v1/devices":
			if r.Header.Get("Authorization") != "Bearer session-test" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(`[{"ID":"dev_test","Name":"Office PC","VirtualIP":"10.90.0.2"}]`))
		default:
			if strings.HasSuffix(r.URL.Path, "/restart") && r.Header.Get("Authorization") == "Bearer session-test" {
				w.WriteHeader(http.StatusAccepted)
				_, _ = w.Write([]byte(`{"ID":"cmd_test","Status":"queued"}`))
				return
			}
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	cfg := config.DefaultClient()
	cfg.ServerURL = server.URL
	cfg.Name = "test"
	cfg.StateDir = t.TempDir()
	client := New(cfg)
	client.startupEnabled = func(application.StartupSpec) (bool, error) { return true, nil }
	var startupValue bool
	client.setStartup = func(_ application.StartupSpec, enabled bool) error { startupValue = enabled; return nil }
	stopped := make(chan struct{}, 1)
	client.ConfigureApplication(application.StartupSpec{}, "test-tray-token", func() { stopped <- struct{}{} })
	h := client.LocalHandler(filepath.Join(t.TempDir(), "client.yaml"))

	r := httptest.NewRequest(http.MethodPost, "/api/management/login", bytes.NewBufferString(`{"username":"operator","password":"secret"}`))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK || !bytes.Contains(w.Body.Bytes(), []byte("session-test")) {
		t.Fatalf("management login: %d %s", w.Code, w.Body.String())
	}

	r = httptest.NewRequest(http.MethodGet, "/api/management/devices", nil)
	r.Header.Set("Authorization", "Bearer session-test")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK || !bytes.Contains(w.Body.Bytes(), []byte("Office PC")) {
		t.Fatalf("management devices: %d %s", w.Code, w.Body.String())
	}

	r = httptest.NewRequest(http.MethodGet, "/api/tray", nil)
	r.RemoteAddr = "192.168.1.50:50000"
	r.Header.Set("X-TyxNet-Tray-Token", "test-tray-token")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("remote tray API status: %d", w.Code)
	}
	r = httptest.NewRequest(http.MethodGet, "/api/tray", nil)
	r.RemoteAddr = "127.0.0.1:50000"
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("missing tray token was accepted: %d", w.Code)
	}
	r = httptest.NewRequest(http.MethodGet, "/api/tray", nil)
	r.RemoteAddr = "127.0.0.1:50000"
	r.Header.Set("X-TyxNet-Tray-Token", "test-tray-token")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK || !bytes.Contains(w.Body.Bytes(), []byte("Office PC")) {
		t.Fatalf("loopback tray API: %d %s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"startup_enabled":true`)) {
		t.Fatalf("startup state missing: %s", w.Body.String())
	}

	r = httptest.NewRequest(http.MethodPost, "/api/tray/startup", bytes.NewBufferString(`{"enabled":true}`))
	r.RemoteAddr = "127.0.0.1:50000"
	r.Header.Set("X-TyxNet-Tray-Token", "test-tray-token")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK || !startupValue {
		t.Fatalf("tray startup: %d %s", w.Code, w.Body.String())
	}

	r = httptest.NewRequest(http.MethodPatch, "/api/management/startup", bytes.NewBufferString(`{"enabled":false}`))
	r.Header.Set("Authorization", "Bearer session-test")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK || startupValue {
		t.Fatalf("web startup: %d %s", w.Code, w.Body.String())
	}

	r = httptest.NewRequest(http.MethodPost, "/api/tray/quit", nil)
	r.RemoteAddr = "127.0.0.1:50000"
	r.Header.Set("X-TyxNet-Tray-Token", "test-tray-token")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("tray quit did not stop the client")
	}
}

func TestRunWaitsForWebSetup(t *testing.T) {
	cfg := config.DefaultClient()
	cfg.ServerURL = "https://placeholder.example"
	cfg.Name = "test"
	cfg.StateDir = t.TempDir()
	client := New(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := client.Run(ctx); err != nil {
		t.Fatalf("cancelled unconfigured client: %v", err)
	}
}
