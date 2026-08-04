package control

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fbeser/tyxnet/internal/application"
	"github.com/fbeser/tyxnet/internal/auth"
	"github.com/fbeser/tyxnet/internal/storage"
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
