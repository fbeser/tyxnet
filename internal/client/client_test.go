package client

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/fbeser/tyxnet/internal/application"
	"github.com/fbeser/tyxnet/internal/config"
	"github.com/fbeser/tyxnet/internal/tunnel"
	"github.com/fbeser/tyxnet/pkg/protocol"
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

func TestControlCommandExecutionAndSignedResults(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	nonce := bytes.Repeat([]byte{7}, 32)
	var statuses []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/control/v1/challenge":
			_ = json.NewEncoder(w).Encode(map[string]string{"challenge": base64.RawStdEncoding.EncodeToString(nonce)})
		case strings.HasPrefix(r.URL.Path, "/control/v1/commands/"):
			var result protocol.CommandResult
			if err := json.NewDecoder(r.Body).Decode(&result); err != nil {
				t.Error(err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			commandID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/control/v1/commands/"), "/result")
			payload, payloadErr := protocol.CommandResultSigningPayload(nonce, result.DeviceID, commandID, result.Status, result.Result, result.Error)
			signature, signatureErr := base64.RawStdEncoding.DecodeString(result.Signature)
			if payloadErr != nil || signatureErr != nil || !ed25519.Verify(publicKey, payload, signature) {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			statuses = append(statuses, result.Status)
			_ = json.NewEncoder(w).Encode(map[string]string{"status": result.Status})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := New(config.Client{ServerURL: server.URL})
	client.key = privateKey
	client.State.DeviceID = "dev_test"
	executions := 0
	client.executeCommand = func(_ context.Context, commandType string) error {
		executions++
		if commandType != "system.restart" {
			t.Fatalf("unexpected command type: %s", commandType)
		}
		return nil
	}
	command := protocol.ControlCommand{ProtocolVersion: protocol.Version, ID: "cmd_test", Type: "system.restart", ExpiresAt: time.Now().Add(time.Minute)}
	if err := client.handleControlCommand(context.Background(), command); err != nil {
		t.Fatal(err)
	}
	if err := client.handleControlCommand(context.Background(), command); err != nil {
		t.Fatal(err)
	}
	if executions != 1 || len(statuses) != 2 || statuses[0] != "accepted" || statuses[1] != "succeeded" {
		t.Fatalf("executions=%d statuses=%v", executions, statuses)
	}
	invalid := protocol.ControlCommand{ProtocolVersion: protocol.Version, ID: "cmd_invalid", Type: "shell.execute", ExpiresAt: time.Now().Add(time.Minute)}
	if err := client.handleControlCommand(context.Background(), invalid); err == nil {
		t.Fatal("non-allowlisted command was accepted")
	}
	if executions != 1 {
		t.Fatal("non-allowlisted command reached the executor")
	}
}

func TestControlCommandFailureAndReconnect(t *testing.T) {
	_, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	statuses := make([]string, 0, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/control/v1/challenge" {
			_ = json.NewEncoder(w).Encode(map[string]string{"challenge": base64.RawStdEncoding.EncodeToString(bytes.Repeat([]byte{9}, 32))})
			return
		}
		var result protocol.CommandResult
		_ = json.NewDecoder(r.Body).Decode(&result)
		statuses = append(statuses, result.Status)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": result.Status})
	}))
	defer server.Close()
	client := New(config.Client{ServerURL: server.URL})
	client.key = privateKey
	client.State.DeviceID = "dev_test"
	client.executeCommand = func(context.Context, string) error { return errors.New("permission denied") }
	failed := protocol.ControlCommand{ProtocolVersion: protocol.Version, ID: "cmd_failed", Type: "system.shutdown", ExpiresAt: time.Now().Add(time.Minute)}
	if err := client.handleControlCommand(context.Background(), failed); err != nil {
		t.Fatal(err)
	}
	reconnect := protocol.ControlCommand{ProtocolVersion: protocol.Version, ID: "cmd_reconnect", Type: "client.reconnect", ExpiresAt: time.Now().Add(time.Minute)}
	if err := client.handleControlCommand(context.Background(), reconnect); !errors.Is(err, errReconnectRequested) {
		t.Fatalf("reconnect result = %v", err)
	}
	want := []string{"accepted", "failed", "accepted", "succeeded"}
	if !slices.Equal(statuses, want) {
		t.Fatalf("statuses=%v want=%v", statuses, want)
	}
}

func TestConnectProcessesCommandEvent(t *testing.T) {
	_, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	nonce := bytes.Repeat([]byte{3}, 32)
	statuses := make(chan string, 2)
	done := make(chan struct{}, 1)
	command := protocol.ControlCommand{ProtocolVersion: protocol.Version, ID: "cmd_stream", Type: "system.shutdown", CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Minute)}
	commandJSON, _ := json.Marshal(command)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/control/v1/challenge":
			_ = json.NewEncoder(w).Encode(map[string]string{"challenge": base64.RawStdEncoding.EncodeToString(nonce)})
		case r.URL.Path == "/control/v1/connect":
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprintf(w, "event: connected\ndata: {\"protocol_version\":1}\n\nevent: command\ndata: %s\n\n", commandJSON)
			w.(http.Flusher).Flush()
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Error("client did not report command completion")
			}
		case strings.HasPrefix(r.URL.Path, "/control/v1/commands/"):
			var result protocol.CommandResult
			_ = json.NewDecoder(r.Body).Decode(&result)
			statuses <- result.Status
			if result.Status == "succeeded" {
				done <- struct{}{}
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"status": result.Status})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	client := New(config.Client{ServerURL: server.URL})
	client.key = privateKey
	client.State.DeviceID = "dev_stream"
	executed := 0
	client.executeCommand = func(_ context.Context, commandType string) error {
		executed++
		if commandType != "system.shutdown" {
			t.Fatalf("command type=%s", commandType)
		}
		return nil
	}
	if err := client.connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	first, second := <-statuses, <-statuses
	if executed != 1 || first != "accepted" || second != "succeeded" {
		t.Fatalf("executed=%d statuses=%s,%s", executed, first, second)
	}
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

func TestEmbeddedClientAssetsIncludeVirtualIPCopy(t *testing.T) {
	cfg := config.DefaultClient()
	cfg.StateDir = t.TempDir()
	h := New(cfg).LocalHandler(filepath.Join(t.TempDir(), "client.yaml"))
	r := httptest.NewRequest(http.MethodGet, "/ui/app.js", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("client app asset: %d", w.Code)
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("function copyText")) || !bytes.Contains(w.Body.Bytes(), []byte("data-copy")) {
		t.Fatal("client virtual IP copy support is missing")
	}
}

func TestManagementProxyAndLoopbackTrayAPI(t *testing.T) {
	var rememberedLogin bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/auth/login":
			var login struct {
				Remember bool `json:"remember"`
			}
			_ = json.NewDecoder(r.Body).Decode(&login)
			rememberedLogin = login.Remember
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

	r := httptest.NewRequest(http.MethodPost, "/api/management/login", bytes.NewBufferString(`{"username":"operator","password":"secret","remember":true}`))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK || !bytes.Contains(w.Body.Bytes(), []byte("session-test")) {
		t.Fatalf("management login: %d %s", w.Code, w.Body.String())
	}
	if !rememberedLogin {
		t.Fatal("management login did not forward remember preference")
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
