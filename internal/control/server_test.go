package control

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fbeser/tyxnet/internal/application"
	"github.com/fbeser/tyxnet/internal/auth"
	"github.com/fbeser/tyxnet/internal/storage"
	"github.com/fbeser/tyxnet/pkg/protocol"
)

func TestAPIAuthorizationAndLogin(t *testing.T) {
	ctx := context.Background()
	st, err := storage.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	ph, _ := auth.HashPassword("a-secure-password")
	if _, err = st.CreateAdmin(ctx, "admin", ph); err != nil {
		t.Fatal(err)
	}
	server := New(st, "10.90.0.0/24", time.Minute, slog.Default(), true)
	server.SetAdapter("TyxNet", "10.90.0.1/24")
	h := server.Handler()
	r := httptest.NewRequest("GET", "/api/v1/devices", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", w.Code)
	}
	body := bytes.NewBufferString(`{"username":"admin","password":"a-secure-password"}`)
	r = httptest.NewRequest("POST", "/api/v1/auth/login", body)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("login: %d %s", w.Code, w.Body.String())
	}
	var login map[string]any
	if err = json.Unmarshal(w.Body.Bytes(), &login); err != nil {
		t.Fatal(err)
	}
	token := login["access_token"].(string)
	r = httptest.NewRequest("GET", "/api/v1/devices", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("authorized: %d %s", w.Code, w.Body.String())
	}
}

func TestDeviceConnectionPresence(t *testing.T) {
	ctx := context.Background()
	st, err := storage.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()

	server := New(st, "10.90.0.0/24", time.Minute, slog.Default(), true)
	devices := []storage.Device{{ID: "device-1"}, {ID: "revoked", Revoked: true}}
	server.deviceConnected("device-1")
	server.deviceConnected("device-1")
	server.deviceConnected("revoked")
	server.setDevicePresence(devices)
	if !devices[0].Online || devices[1].Online {
		t.Fatalf("unexpected presence state: %+v", devices)
	}

	server.deviceDisconnected("device-1")
	if !server.deviceOnline("device-1") {
		t.Fatal("one of two live connections must keep the device online")
	}
	server.deviceDisconnected("device-1")
	if server.deviceOnline("device-1") {
		t.Fatal("device must become offline after its final connection closes")
	}
}

func TestLocalWebBootstrap(t *testing.T) {
	ctx := context.Background()
	st, err := storage.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	server := New(st, "10.90.0.0/24", time.Minute, slog.Default(), true)
	server.SetAdapter("TyxNet", "10.90.0.1/24")
	h := server.Handler()

	r := httptest.NewRequest("GET", "/api/v1/setup/status", nil)
	r.RemoteAddr = "127.0.0.1:50000"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte(`"required":true`)) {
		t.Fatalf("status: %d %s", w.Code, w.Body.String())
	}

	body := bytes.NewBufferString(`{"Username":"admin","Password":"a-secure-password"}`)
	r = httptest.NewRequest("POST", "/api/v1/setup", body)
	r.RemoteAddr = "127.0.0.1:50000"
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("setup: %d %s", w.Code, w.Body.String())
	}
	var session struct {
		AccessToken string       `json:"access_token"`
		User        storage.User `json:"user"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &session); err != nil || session.AccessToken == "" {
		t.Fatalf("setup response: %v %s", err, w.Body.String())
	}

	body = bytes.NewBufferString(`{"Username":"viewer","Password":"another-secure-password","Role":"viewer"}`)
	r = httptest.NewRequest("POST", "/api/v1/users", body)
	r.Header.Set("Authorization", "Bearer "+session.AccessToken)
	r.RemoteAddr = "127.0.0.1:50000"
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("create user: %d %s", w.Code, w.Body.String())
	}
	var viewer storage.User
	if err := json.Unmarshal(w.Body.Bytes(), &viewer); err != nil {
		t.Fatal(err)
	}

	body = bytes.NewBufferString(`{"Disabled":true,"Role":"operator"}`)
	r = httptest.NewRequest("PATCH", "/api/v1/users/"+viewer.ID, body)
	r.Header.Set("Authorization", "Bearer "+session.AccessToken)
	r.RemoteAddr = "127.0.0.1:50000"
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("update user: %d %s", w.Code, w.Body.String())
	}

	for _, path := range []string{"/api/v1/commands", "/api/v1/audit", "/api/v1/server/status"} {
		r = httptest.NewRequest("GET", path, nil)
		r.Header.Set("Authorization", "Bearer "+session.AccessToken)
		r.RemoteAddr = "127.0.0.1:50000"
		w = httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("GET %s: %d %s", path, w.Code, w.Body.String())
		}
	}
	r = httptest.NewRequest("GET", "/api/v1/server/status", nil)
	r.Header.Set("Authorization", "Bearer "+session.AccessToken)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if !bytes.Contains(w.Body.Bytes(), []byte(`"adapter_ready":true`)) || !bytes.Contains(w.Body.Bytes(), []byte(`"adapter_name":"TyxNet"`)) {
		t.Fatalf("adapter status missing: %s", w.Body.String())
	}

	body = bytes.NewBufferString(`{"Username":"second","Password":"a-secure-password"}`)
	r = httptest.NewRequest("POST", "/api/v1/setup", body)
	r.RemoteAddr = "127.0.0.1:50001"
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusConflict {
		t.Fatalf("second setup: %d %s", w.Code, w.Body.String())
	}
}

func TestAdminUpdatesUserPassword(t *testing.T) {
	ctx := context.Background()
	st, err := storage.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	adminHash, _ := auth.HashPassword("admin-secure-password")
	viewerHash, _ := auth.HashPassword("viewer-old-password")
	admin, err := st.CreateInitialAdmin(ctx, "admin", adminHash)
	if err != nil {
		t.Fatal(err)
	}
	viewer, err := st.CreateUser(ctx, "viewer", viewerHash, "viewer")
	if err != nil {
		t.Fatal(err)
	}
	adminSession, _ := st.CreateSession(ctx, admin.ID, time.Hour)
	viewerSession, _ := st.CreateSession(ctx, viewer.ID, time.Hour)
	h := New(st, "10.90.0.0/24", time.Minute, slog.Default(), true).Handler()

	request := func(token, userID, password string) *httptest.ResponseRecorder {
		t.Helper()
		body, err := json.Marshal(map[string]string{"password": password})
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest(http.MethodPut, "/api/v1/users/"+userID+"/password", bytes.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}

	if w := request(viewerSession, viewer.ID, "viewer-new-password"); w.Code != http.StatusForbidden {
		t.Fatalf("viewer password update: %d %s", w.Code, w.Body.String())
	}
	if w := request(adminSession, viewer.ID, "too-short"); w.Code != http.StatusBadRequest {
		t.Fatalf("short password update: %d %s", w.Code, w.Body.String())
	}
	_, unchangedHash, err := st.Authenticate(ctx, viewer.Username)
	if err != nil || !auth.VerifyPassword(unchangedHash, "viewer-old-password") {
		t.Fatalf("invalid update changed password: %v", err)
	}
	if w := request(adminSession, viewer.ID, "viewer-new-password"); w.Code != http.StatusOK {
		t.Fatalf("admin password update: %d %s", w.Code, w.Body.String())
	}
	_, changedHash, err := st.Authenticate(ctx, viewer.Username)
	if err != nil || auth.VerifyPassword(changedHash, "viewer-old-password") || !auth.VerifyPassword(changedHash, "viewer-new-password") {
		t.Fatalf("password verification after update failed: %v", err)
	}
	if _, err = st.SessionUser(ctx, viewerSession); err == nil {
		t.Fatal("password update did not revoke the viewer session")
	}
	if w := request(adminSession, "missing-user", "missing-new-password"); w.Code != http.StatusNotFound {
		t.Fatalf("missing user password update: %d %s", w.Code, w.Body.String())
	}
	logs, err := st.ListAudit(ctx, 10)
	if err != nil || len(logs) != 1 || logs[0].Action != "user.password.update" || logs[0].TargetID != viewer.ID {
		t.Fatalf("password audit entry: %+v %v", logs, err)
	}
}

func TestWebBootstrapRejectsRemoteRequests(t *testing.T) {
	st, err := storage.Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	h := New(st, "10.90.0.0/24", time.Minute, slog.Default(), true).Handler()
	r := httptest.NewRequest("POST", "/api/v1/setup", bytes.NewBufferString(`{"Username":"admin","Password":"a-secure-password"}`))
	r.RemoteAddr = "203.0.113.10:50000"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("remote setup: %d %s", w.Code, w.Body.String())
	}
}

func TestWebBootstrapAllowsExplicitRemoteSetup(t *testing.T) {
	st, err := storage.Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	server := New(st, "10.90.0.0/24", time.Minute, slog.Default(), true)
	server.AllowRemoteBootstrap()
	h := server.Handler()

	r := httptest.NewRequest("GET", "/api/v1/setup/status", nil)
	r.RemoteAddr = "192.168.1.50:50000"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK || !bytes.Contains(w.Body.Bytes(), []byte(`"enabled":true`)) {
		t.Fatalf("remote setup status: %d %s", w.Code, w.Body.String())
	}
}

func TestMemberOnlySeesOwnedDevices(t *testing.T) {
	ctx := context.Background()
	st, err := storage.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	admin, _ := st.CreateInitialAdmin(ctx, "admin", "hash")
	member, _ := st.CreateUser(ctx, "member", "hash", "member")
	for _, owner := range []storage.User{admin, member} {
		_, enrollment, _ := st.CreateEnrollmentToken(ctx, owner.ID, time.Hour, 1)
		if _, err := st.JoinDevice(ctx, enrollment, owner.Username+"-pc", "linux", "arm64", "dev", "10.90.0.0/24", []byte("key")); err != nil {
			t.Fatal(err)
		}
	}
	session, _ := st.CreateSession(ctx, member.ID, time.Hour)
	h := New(st, "10.90.0.0/24", time.Minute, slog.Default(), true).Handler()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/devices", nil)
	r.Header.Set("Authorization", "Bearer "+session)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK || !bytes.Contains(w.Body.Bytes(), []byte("member-pc")) || bytes.Contains(w.Body.Bytes(), []byte("admin-pc")) {
		t.Fatalf("member device scope: %d %s", w.Code, w.Body.String())
	}
}

func TestNetworkFlowsExposeRoutedMetadataByRole(t *testing.T) {
	ctx := context.Background()
	st, err := storage.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	admin, _ := st.CreateInitialAdmin(ctx, "admin", "hash")
	member, _ := st.CreateUser(ctx, "member", "hash", "member")
	deviceIDs := make([]string, 0, 2)
	for _, owner := range []storage.User{admin, member} {
		_, enrollment, createErr := st.CreateEnrollmentToken(ctx, owner.ID, time.Hour, 1)
		if createErr != nil {
			t.Fatal(createErr)
		}
		device, joinErr := st.JoinDevice(ctx, enrollment, owner.Username+"-device", "linux", "arm64", "dev", "10.90.0.0/24", []byte("key"))
		if joinErr != nil {
			t.Fatal(joinErr)
		}
		deviceIDs = append(deviceIDs, device.VirtualIP)
	}
	adminSession, _ := st.CreateSession(ctx, admin.ID, time.Hour)
	memberSession, _ := st.CreateSession(ctx, member.ID, time.Hour)
	server := New(st, "10.90.0.0/24", time.Minute, slog.Default(), true)
	server.SetAdapter("TyxNet", "10.90.0.1/24")
	server.TrafficMonitor().SetReady(true)
	server.TrafficMonitor().Observe(net.ParseIP(deviceIDs[0]), net.ParseIP(deviceIDs[1]), 1_000_000)
	h := server.Handler()

	r := httptest.NewRequest(http.MethodGet, "/api/v1/network/flows", nil)
	r.Header.Set("Authorization", "Bearer "+adminSession)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK || !bytes.Contains(w.Body.Bytes(), []byte(`"data_plane_ready":true`)) || !bytes.Contains(w.Body.Bytes(), []byte(`"admin-device"`)) || !bytes.Contains(w.Body.Bytes(), []byte(`"mbps":1.6`)) {
		t.Fatalf("network flows: %d %s", w.Code, w.Body.String())
	}

	r = httptest.NewRequest(http.MethodGet, "/api/v1/network/flows", nil)
	r.Header.Set("Authorization", "Bearer "+memberSession)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("member network flows: %d %s", w.Code, w.Body.String())
	}
}

func TestCommandDeliveryAndAuthenticatedResults(t *testing.T) {
	ctx := context.Background()
	st, err := storage.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	admin, _ := st.CreateInitialAdmin(ctx, "admin", "hash")
	publicKey, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	_, enrollment, _ := st.CreateEnrollmentToken(ctx, admin.ID, time.Hour, 1)
	device, err := st.JoinDevice(ctx, enrollment, "laptop", "darwin", "arm64", "dev", "10.90.0.0/24", publicKey)
	if err != nil {
		t.Fatal(err)
	}
	command, err := st.CreateCommand(ctx, admin.ID, device.ID, "system.restart", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	server := New(st, "10.90.0.0/24", time.Minute, slog.Default(), true)
	var stream bytes.Buffer
	if err = server.writePendingCommands(ctx, &stream, device.ID); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(stream.Bytes(), []byte("event: command")) || !bytes.Contains(stream.Bytes(), []byte(command.ID)) || !bytes.Contains(stream.Bytes(), []byte("system.restart")) {
		t.Fatalf("command event: %s", stream.String())
	}
	commands, _ := st.ListCommands(ctx, 10)
	if len(commands) != 1 || commands[0].Status != "delivered" {
		t.Fatalf("delivered status: %+v", commands)
	}
	h := server.Handler()
	challenge := func() []byte {
		t.Helper()
		request := httptest.NewRequest(http.MethodPost, "/control/v1/challenge", bytes.NewBufferString(`{"device_id":"`+device.ID+`"}`))
		response := httptest.NewRecorder()
		h.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("challenge: %d %s", response.Code, response.Body.String())
		}
		var body struct {
			Challenge string `json:"challenge"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		nonce, decodeErr := base64.RawStdEncoding.DecodeString(body.Challenge)
		if decodeErr != nil {
			t.Fatal(decodeErr)
		}
		return nonce
	}
	report := func(status, resultText, errorText string, signature []byte) *httptest.ResponseRecorder {
		t.Helper()
		body, marshalErr := json.Marshal(protocol.CommandResult{DeviceID: device.ID, Status: status, Result: resultText, Error: errorText, Signature: base64.RawStdEncoding.EncodeToString(signature)})
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		request := httptest.NewRequest(http.MethodPost, "/control/v1/commands/"+command.ID+"/result", bytes.NewReader(body))
		response := httptest.NewRecorder()
		h.ServeHTTP(response, request)
		return response
	}

	_ = challenge()
	if response := report("accepted", "", "", make([]byte, ed25519.SignatureSize)); response.Code != http.StatusForbidden {
		t.Fatalf("forged command result: %d %s", response.Code, response.Body.String())
	}
	for _, update := range []struct {
		status string
		result string
	}{
		{status: "accepted"},
		{status: "succeeded", result: "system action scheduled"},
	} {
		nonce := challenge()
		payload, payloadErr := protocol.CommandResultSigningPayload(nonce, device.ID, command.ID, update.status, update.result, "")
		if payloadErr != nil {
			t.Fatal(payloadErr)
		}
		if response := report(update.status, update.result, "", ed25519.Sign(privateKey, payload)); response.Code != http.StatusOK {
			t.Fatalf("%s result: %d %s", update.status, response.Code, response.Body.String())
		}
		if update.status == "accepted" {
			if response := report(update.status, update.result, "", ed25519.Sign(privateKey, payload)); response.Code != http.StatusForbidden {
				t.Fatalf("replayed result proof: %d %s", response.Code, response.Body.String())
			}
		}
	}
	commands, _ = st.ListCommands(ctx, 10)
	if len(commands) != 1 || commands[0].Status != "succeeded" || commands[0].Result != "system action scheduled" {
		t.Fatalf("final command state: %+v", commands)
	}
}

func TestEmbeddedConsoleAssets(t *testing.T) {
	st, err := storage.Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	h := New(st, "10.90.0.0/24", time.Minute, slog.Default(), true).Handler()
	for _, path := range []string{"/", "/ui/app.css", "/ui/startup.css", "/ui/app.js"} {
		r := httptest.NewRequest("GET", path, nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 200 || w.Body.Len() == 0 {
			t.Fatalf("asset %s: %d", path, w.Code)
		}
	}
	r := httptest.NewRequest("GET", "/ui/app.css", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if !bytes.Contains(w.Body.Bytes(), []byte("[hidden]{display:none!important}")) {
		t.Fatal("hidden visibility guard is missing")
	}
	r = httptest.NewRequest("GET", "/ui/app.js", nil)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if !bytes.Contains(w.Body.Bytes(), []byte("Change password")) {
		t.Fatal("password update action is missing")
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("Network flows")) || !bytes.Contains(w.Body.Bytes(), []byte("renderFlowMap")) {
		t.Fatal("network flow panel is missing")
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("Array.isArray(raw.flows)")) {
		t.Fatal("network flow response normalization is missing")
	}
}

func TestAdminUpdatesStaticIPAndPingInterval(t *testing.T) {
	ctx := context.Background()
	st, err := storage.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	admin, _ := st.CreateInitialAdmin(ctx, "admin", "hash")
	_, enrollment, _ := st.CreateEnrollmentToken(ctx, admin.ID, time.Hour, 1)
	device, err := st.JoinDevice(ctx, enrollment, "laptop", "windows", "amd64", "dev", "10.90.0.0/24", []byte("key"))
	if err != nil {
		t.Fatal(err)
	}
	session, _ := st.CreateSession(ctx, admin.ID, time.Hour)
	h := New(st, "10.90.0.0/24", time.Minute, slog.Default(), true).Handler()

	r := httptest.NewRequest(http.MethodPatch, "/api/v1/devices/"+device.ID, bytes.NewBufferString(`{"VirtualIP":"10.90.0.42"}`))
	r.Header.Set("Authorization", "Bearer "+session)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("static IP update: %d %s", w.Code, w.Body.String())
	}

	r = httptest.NewRequest(http.MethodPatch, "/api/v1/server/settings", bytes.NewBufferString(`{"ping_interval_seconds":60}`))
	r.Header.Set("Authorization", "Bearer "+session)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("ping interval update: %d %s", w.Code, w.Body.String())
	}
	r = httptest.NewRequest(http.MethodGet, "/api/v1/server/status", nil)
	r.Header.Set("Authorization", "Bearer "+session)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK || !bytes.Contains(w.Body.Bytes(), []byte(`"ping_interval_seconds":60`)) {
		t.Fatalf("ping interval status: %d %s", w.Code, w.Body.String())
	}

	h = New(st, "10.90.0.0/24", time.Minute, slog.Default(), true).Handler()
	r = httptest.NewRequest(http.MethodGet, "/api/v1/server/settings", nil)
	r.Header.Set("Authorization", "Bearer "+session)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK || !bytes.Contains(w.Body.Bytes(), []byte(`"ping_interval_seconds":60`)) {
		t.Fatalf("persisted ping interval: %d %s", w.Code, w.Body.String())
	}
}

func TestAuthenticatedLocalTrayControlsStartupAndShutdown(t *testing.T) {
	st, err := storage.Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	admin, _ := st.CreateInitialAdmin(context.Background(), "admin", "hash")
	session, _ := st.CreateSession(context.Background(), admin.ID, time.Hour)
	server := New(st, "10.90.0.0/24", time.Minute, slog.Default(), true)
	server.startupAvailable = func() (bool, string) { return true, "" }
	server.startupEnabled = func(application.StartupSpec) (bool, error) { return true, nil }
	var startupValue bool
	server.setStartup = func(_ application.StartupSpec, enabled bool) error { startupValue = enabled; return nil }
	stopped := make(chan struct{}, 1)
	server.ConfigureApplication(application.StartupSpec{ID: "test"}, "tray-secret", func() { stopped <- struct{}{} })
	h := server.Handler()

	r := httptest.NewRequest(http.MethodGet, "/api/tray", nil)
	r.RemoteAddr = "127.0.0.1:50000"
	r.Header.Set("X-TyxNet-Tray-Token", "wrong-secret")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("invalid tray token was accepted: %d", w.Code)
	}

	r = httptest.NewRequest(http.MethodGet, "/api/tray", nil)
	r.RemoteAddr = "127.0.0.1:50000"
	r.Header.Set("X-TyxNet-Tray-Token", "tray-secret")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK || !bytes.Contains(w.Body.Bytes(), []byte(`"startup_enabled":true`)) {
		t.Fatalf("tray status: %d %s", w.Code, w.Body.String())
	}

	r = httptest.NewRequest(http.MethodPost, "/api/tray/startup", bytes.NewBufferString(`{"enabled":true}`))
	r.RemoteAddr = "127.0.0.1:50000"
	r.Header.Set("X-TyxNet-Tray-Token", "tray-secret")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK || !startupValue {
		t.Fatalf("tray startup: %d %s", w.Code, w.Body.String())
	}

	r = httptest.NewRequest(http.MethodPatch, "/api/v1/server/startup", bytes.NewBufferString(`{"enabled":false}`))
	r.Header.Set("Authorization", "Bearer "+session)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK || startupValue {
		t.Fatalf("web startup: %d %s", w.Code, w.Body.String())
	}

	r = httptest.NewRequest(http.MethodPost, "/api/tray/quit", nil)
	r.RemoteAddr = "127.0.0.1:50000"
	r.Header.Set("X-TyxNet-Tray-Token", "tray-secret")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("tray quit did not stop the application")
	}
}

func TestContainerStartupIsUnavailableWithoutSystemCalls(t *testing.T) {
	ctx := context.Background()
	st, err := storage.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	admin, _ := st.CreateInitialAdmin(ctx, "admin", "hash")
	session, _ := st.CreateSession(ctx, admin.ID, time.Hour)
	server := New(st, "10.90.0.0/24", time.Minute, slog.Default(), true)
	server.startupAvailable = func() (bool, string) {
		return false, "startup is managed by the container runtime"
	}
	server.startupEnabled = func(application.StartupSpec) (bool, error) {
		t.Fatal("startup state must not call systemctl when unavailable")
		return false, nil
	}
	server.setStartup = func(application.StartupSpec, bool) error {
		t.Fatal("startup update must not write a systemd unit when unavailable")
		return nil
	}
	h := server.Handler()

	r := httptest.NewRequest(http.MethodGet, "/api/v1/server/startup", nil)
	r.Header.Set("Authorization", "Bearer "+session)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK || !bytes.Contains(w.Body.Bytes(), []byte(`"available":false`)) {
		t.Fatalf("container startup state: %d %s", w.Code, w.Body.String())
	}

	r = httptest.NewRequest(http.MethodPatch, "/api/v1/server/startup", bytes.NewBufferString(`{"enabled":true}`))
	r.Header.Set("Authorization", "Bearer "+session)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusConflict || !bytes.Contains(w.Body.Bytes(), []byte(`"code":"startup_unavailable"`)) {
		t.Fatalf("container startup update: %d %s", w.Code, w.Body.String())
	}
}
