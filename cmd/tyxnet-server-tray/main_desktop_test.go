//go:build windows || darwin

package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCheckServerAndTrayIcon(t *testing.T) {
	trayToken = "test-token"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tray" || r.Header.Get("X-TyxNet-Tray-Token") != trayToken {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"running":true,"startup_enabled":true}`))
	}))
	defer server.Close()
	serverURL = server.URL
	if snapshot, err := checkServer(); err != nil || !snapshot.Running || !snapshot.StartupEnabled {
		t.Fatal(err)
	}
	if len(trayIcon()) < 100 {
		t.Fatal("tray icon is unexpectedly empty")
	}
}
