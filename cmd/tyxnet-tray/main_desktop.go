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
	"time"

	"fyne.io/systray"
)

type trayDevice struct {
	ID, Name, VirtualIP string
	Revoked             bool
	LastSeen            *time.Time
}
type traySnapshot struct {
	Local struct {
		Configured bool   `json:"configured"`
		Connected  bool   `json:"connected"`
		VirtualIP  string `json:"virtual_ip"`
		LastError  string `json:"last_error"`
	} `json:"local"`
	User struct {
		Username string
		Role     string
	} `json:"user"`
	Devices        []trayDevice `json:"devices"`
	StartupEnabled bool         `json:"startup_enabled"`
}

var (
	clientURL string
	trayToken = os.Getenv("TYXNET_TRAY_TOKEN")
	httpc     = &http.Client{Timeout: 5 * time.Second}
)

func main() {
	flag.StringVar(&clientURL, "client-url", "http://127.0.0.1:9070", "local TyxNet Client web URL")
	flag.Parse()
	systray.Run(onReady, func() {})
}

func onReady() {
	systray.SetTemplateIcon(trayIcon(), trayIcon())
	systray.SetTitle("TyxNet")
	systray.SetTooltip("TyxNet Client")
	systray.SetOnTapped(func() { _ = openBrowser(clientURL) })

	status := systray.AddMenuItem("Checking connection…", "TyxNet connection status")
	status.Disable()
	open := systray.AddMenuItem("Open Web Console", "Open TyxNet in the default browser")
	devicesMenu := systray.AddMenuItem("Devices", "Devices visible to your current role")
	systray.AddSeparator()
	startup := systray.AddMenuItemCheckbox("Run at startup", "Start TyxNet automatically after sign-in", false)
	quit := systray.AddMenuItem("Quit TyxNet", "Stop the client and close the tray")

	go func() {
		for range open.ClickedCh {
			_ = openBrowser(clientURL)
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
	go refreshLoop(status, devicesMenu, startup)
}

func refreshLoop(status, devicesMenu, startup *systray.MenuItem) {
	var children []*systray.MenuItem
	refresh := func() {
		for _, item := range children {
			item.Remove()
		}
		children = nil
		snapshot, err := fetchSnapshot()
		if err != nil {
			status.SetTitle("Client service unavailable")
			children = append(children, disabledChild(devicesMenu, "Open the web console to start TyxNet"))
			return
		}
		setChecked(startup, snapshot.StartupEnabled)
		if snapshot.Local.Connected {
			status.SetTitle("Connected · " + snapshot.Local.VirtualIP)
		} else if snapshot.Local.Configured {
			status.SetTitle("Connecting…")
		} else {
			status.SetTitle("Setup required")
		}
		if snapshot.User.Role == "" {
			children = append(children, disabledChild(devicesMenu, "Sign in through the web console"))
			return
		}
		if len(snapshot.Devices) == 0 {
			children = append(children, disabledChild(devicesMenu, "No visible devices"))
			return
		}
		canControl := snapshot.User.Role == "admin" || snapshot.User.Role == "operator"
		for _, device := range snapshot.Devices {
			title := device.Name + " · " + device.VirtualIP
			item := devicesMenu.AddSubMenuItem(title, device.ID)
			children = append(children, item)
			if !canControl || device.Revoked {
				item.Disable()
				continue
			}
			for _, action := range []string{"disconnect", "restart", "shutdown"} {
				actionItem := item.AddSubMenuItem(actionTitle(action), "Queue "+action)
				go func(id, requestedAction string, clicks <-chan struct{}) {
					for range clicks {
						_ = sendCommand(id, requestedAction)
					}
				}(device.ID, action, actionItem.ClickedCh)
			}
		}
	}
	refresh()
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		refresh()
	}
}

func disabledChild(parent *systray.MenuItem, title string) *systray.MenuItem {
	item := parent.AddSubMenuItem(title, title)
	item.Disable()
	return item
}

func fetchSnapshot() (traySnapshot, error) {
	var snapshot traySnapshot
	request, err := http.NewRequest(http.MethodGet, clientURL+"/api/tray", nil)
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
		return snapshot, fmt.Errorf("tray API: %s", response.Status)
	}
	err = json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&snapshot)
	return snapshot, err
}

func sendCommand(deviceID, action string) error {
	request, err := http.NewRequest(http.MethodPost, clientURL+"/api/tray/devices/"+deviceID+"/"+action, bytes.NewReader(nil))
	if err != nil {
		return err
	}
	request.Header.Set("X-TyxNet-Tray-Token", trayToken)
	response, err := httpc.Do(request)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("tray command: %s", response.Status)
	}
	return nil
}

func setApplicationState(action string, enabled bool) error {
	var body []byte
	if action == "startup" {
		body, _ = json.Marshal(map[string]bool{"enabled": enabled})
	}
	request, err := http.NewRequest(http.MethodPost, clientURL+"/api/tray/"+action, bytes.NewReader(body))
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

func actionTitle(action string) string {
	switch action {
	case "disconnect":
		return "Reconnect"
	case "restart":
		return "Restart"
	default:
		return "Shutdown"
	}
}

func openBrowser(url string) error {
	if runtime.GOOS == "windows" {
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	}
	return exec.Command("open", url).Start()
}
