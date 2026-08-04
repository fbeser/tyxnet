//go:build windows || darwin

package main

import "testing"

func TestTrayIconAndLabels(t *testing.T) {
	if len(trayIcon()) < 100 {
		t.Fatal("tray icon is unexpectedly empty")
	}
	if actionTitle("disconnect") != "Reconnect" || actionTitle("shutdown") != "Shutdown" {
		t.Fatal("unexpected tray action labels")
	}
}
