//go:build windows || darwin

package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"fyne.io/systray"
)

var (
	serverURL string
	trayToken = os.Getenv("TYXNET_TRAY_TOKEN")
	httpc     = &http.Client{Timeout: 5 * time.Second}
)

func main() {
	flag.StringVar(&serverURL, "server-url", "http://127.0.0.1:8443", "local TyxNet Server web URL")
	flag.Parse()
	serverURL = strings.TrimRight(serverURL, "/")
	systray.Run(onReady, func() {})
}

func onReady() {
	systray.SetTemplateIcon(trayIcon(), trayIcon())
	systray.SetTitle("TyxNet Server")
	systray.SetTooltip("TyxNet Server")
	systray.SetOnTapped(func() { _ = openBrowser(serverURL) })

	status := systray.AddMenuItem("Checking server...", "TyxNet Server status")
	status.Disable()
	open := systray.AddMenuItem("Open Web Console", "Open the server console in the default browser")
	systray.AddSeparator()
	startup := systray.AddMenuItemCheckbox("Run at startup", "Start TyxNet automatically after sign-in", false)
	quit := systray.AddMenuItem("Quit TyxNet", "Stop the server and close the tray")

	go func() {
		for range open.ClickedCh {
			_ = openBrowser(serverURL)
		}
	}()
	go func() {
		for range startup.ClickedCh {
			enabled := !startup.Checked()
			if setApplicationState("startup", enabled) == nil {
				setChecked(startup, enabled)
			}
		}
	}()
	go func() {
		<-quit.ClickedCh
		if setApplicationState("quit", false) == nil {
			systray.Quit()
		}
	}()
	go refreshLoop(status, startup)
}

func refreshLoop(status, startup *systray.MenuItem) {
	refresh := func() {
		snapshot, err := checkServer()
		if err != nil {
			status.SetTitle("Server unavailable")
			status.SetTooltip(err.Error())
			return
		}
		status.SetTitle("Server running")
		status.SetTooltip(serverURL)
		setChecked(startup, snapshot.StartupEnabled)
	}
	refresh()
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		refresh()
	}
}

type serverSnapshot struct {
	Running        bool `json:"running"`
	StartupEnabled bool `json:"startup_enabled"`
}

func checkServer() (serverSnapshot, error) {
	var snapshot serverSnapshot
	request, err := http.NewRequest(http.MethodGet, serverURL+"/api/tray", nil)
	if err != nil {
		return snapshot, err
	}
	request.Header.Set("X-TyxNet-Tray-Token", trayToken)
	response, err := httpc.Do(request)
	if err != nil {
		return snapshot, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return snapshot, fmt.Errorf("tray status: %s", response.Status)
	}
	err = json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&snapshot)
	return snapshot, err
}

func setApplicationState(action string, enabled bool) error {
	var body []byte
	if action == "startup" {
		body, _ = json.Marshal(map[string]bool{"enabled": enabled})
	}
	request, err := http.NewRequest(http.MethodPost, serverURL+"/api/tray/"+action, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-TyxNet-Tray-Token", trayToken)
	response, err := httpc.Do(request)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("tray application action: %s", response.Status)
	}
	return nil
}

func setChecked(item *systray.MenuItem, checked bool) {
	if checked {
		item.Check()
	} else {
		item.Uncheck()
	}
}

func openBrowser(url string) error {
	if runtime.GOOS == "windows" {
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	}
	return exec.Command("open", url).Start()
}
