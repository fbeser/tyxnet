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

	"github.com/fbeser/tyxnet/internal/auth"
	"github.com/fbeser/tyxnet/internal/storage"
)

func TestAPIAuthorizationAndLogin(t *testing.T) {
	ctx := context.Background()
	st, err := storage.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ph, _ := auth.HashPassword("a-secure-password")
	if _, err = st.CreateAdmin(ctx, "admin", ph); err != nil {
		t.Fatal(err)
	}
	h := New(st, "10.90.0.0/24", time.Minute, slog.Default()).Handler()
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
