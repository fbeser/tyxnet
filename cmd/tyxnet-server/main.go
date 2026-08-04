package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/fbeser/tyxnet/internal/application"
	"github.com/fbeser/tyxnet/internal/auth"
	"github.com/fbeser/tyxnet/internal/config"
	"github.com/fbeser/tyxnet/internal/control"
	"github.com/fbeser/tyxnet/internal/installer"
	"github.com/fbeser/tyxnet/internal/platform"
	"github.com/fbeser/tyxnet/internal/storage"
	"gopkg.in/yaml.v3"
)

const version = "dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		slog.Error("command failed", "error", err)
		os.Exit(1)
	}
}
func run(args []string) error {
	if len(args) == 0 {
		return usage()
	}
	switch args[0] {
	case "run":
		return runServer(args[1:])
	case "admin":
		if len(args) > 1 && args[1] == "create" {
			return adminCreate(args[2:])
		}
	case "token":
		if len(args) > 1 && args[1] == "create" {
			return tokenCreate(args[2:])
		}
	case "install":
		return install(args[1:])
	case "uninstall":
		return installer.Uninstall("tyxnet-server", "tyxnet-server", "/etc/tyxnet/server.yaml")
	case "start", "stop", "restart", "status", "logs":
		return installer.ServiceAction("tyxnet-server", args[0])
	case "version":
		fmt.Println(version)
		return nil
	}
	return usage()
}
func usage() error {
	fmt.Fprintln(os.Stderr, "usage: tyxnet-server <run|admin create|token create|install|uninstall|start|stop|restart|status|logs|version>")
	return errors.New("invalid command")
}
func runFlags(args []string) (string, string, error) {
	f := flag.NewFlagSet("run", flag.ContinueOnError)
	p := f.String("config", "configs/server.yaml", "configuration file")
	localWeb := f.Bool("local-web", false, "restrict the web console and first setup to this machine")
	lanWeb := f.Bool("lan-web", false, "explicitly expose the web console on the LAN (deprecated; this is the default)")
	if err := f.Parse(args); err != nil {
		return "", "", err
	}
	if *localWeb && *lanWeb {
		return "", "", errors.New("--local-web and --lan-web cannot be used together")
	}
	mode := "config"
	if *localWeb {
		mode = "local"
	} else if *lanWeb {
		mode = "lan"
	}
	return *p, mode, nil
}
func open(path string) (config.Server, *storage.Store, error) {
	c, err := config.LoadServer(path)
	if err != nil {
		return c, nil, err
	}
	s, err := storage.Open(context.Background(), c.Database)
	return c, s, err
}
func runServer(args []string) error {
	path, webMode, err := runFlags(args)
	if err != nil {
		return err
	}
	c, s, err := open(path)
	if err != nil {
		return err
	}
	defer func() { _ = s.Close() }()
	switch webMode {
	case "local":
		c.ListenAddress = "127.0.0.1"
		c.AllowRemoteSetup = false
	case "lan":
		c.ListenAddress = "0.0.0.0"
		c.AllowInsecureHTTP = true
		c.AllowRemoteSetup = true
	}
	if err := c.Validate(); err != nil {
		return err
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	loopback := net.ParseIP(c.ListenAddress).IsLoopback()
	controlServer := control.New(s, c.Network, c.SessionTTL, log, loopback || c.AllowRemoteSetup)
	controlServer.SetDefaultPingInterval(c.PingInterval)
	if c.AllowRemoteSetup && !loopback {
		controlServer.AllowRemoteBootstrap()
		log.Warn("remote first-admin setup enabled; use only on a trusted LAN")
	}
	if c.TunnelEnabled {
		_, network, parseErr := net.ParseCIDR(c.Network)
		if parseErr != nil {
			return fmt.Errorf("parse tunnel network: %w", parseErr)
		}
		ones, _ := network.Mask.Size()
		addressCIDR := fmt.Sprintf("%s/%d", c.TunnelAddress, ones)
		device, tunnelErr := platform.EnsureTunnel(context.Background(), c.TunnelName, addressCIDR, c.TunnelMTU)
		if tunnelErr != nil {
			return fmt.Errorf("ensure server virtual adapter (set tunnel_enabled: false for control-plane-only mode): %w", tunnelErr)
		}
		defer func() { _ = device.Close() }()
		controlServer.SetAdapter(device.Name(), addressCIDR)
		log.Info("virtual adapter ready", "name", device.Name(), "address", addressCIDR, "mtu", c.TunnelMTU)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	executable, _ := os.Executable()
	executable, _ = filepath.Abs(executable)
	workingDirectory, _ := os.Getwd()
	configPath, _ := filepath.Abs(path)
	companion := filepath.Join(filepath.Dir(executable), "tyxnet-server-tray")
	if runtime.GOOS == "windows" {
		companion += ".exe"
	} else if runtime.GOOS != "darwin" {
		companion = ""
	}
	trayToken := application.TrayToken()
	webScheme := "http"
	if c.TLSCert != "" {
		webScheme = "https"
	}
	startupArgs := []string{"run", "--config", configPath}
	switch webMode {
	case "local":
		startupArgs = append(startupArgs, "--local-web")
	case "lan":
		startupArgs = append(startupArgs, "--lan-web")
	}
	controlServer.ConfigureApplication(application.StartupSpec{ID: "tyxnet-server", DisplayName: "TyxNet Server", Executable: executable, WorkingDirectory: workingDirectory, Arguments: startupArgs, Companion: companion, CompanionArgs: []string{"--server-url", fmt.Sprintf("%s://127.0.0.1:%d", webScheme, c.APIPort)}, TrayToken: trayToken}, trayToken, stop)
	h := controlServer.Handler()
	srv := &http.Server{Addr: fmt.Sprintf("%s:%d", c.ListenAddress, c.APIPort), Handler: h, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 0, IdleTimeout: 60 * time.Second}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	log.Info("server starting", "address", srv.Addr, "tls", c.TLSCert != "")
	if c.TLSCert != "" && c.TLSKey != "" {
		err = srv.ListenAndServeTLS(c.TLSCert, c.TLSKey)
	} else {
		log.Warn("TLS disabled; bind locally or use a TLS reverse proxy")
		err = srv.ListenAndServe()
	}
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
func adminCreate(args []string) error {
	f := flag.NewFlagSet("admin create", flag.ContinueOnError)
	cfg := f.String("config", "configs/server.yaml", "")
	username := f.String("username", "admin", "")
	stdin := f.Bool("password-stdin", false, "")
	if err := f.Parse(args); err != nil {
		return err
	}
	if !*stdin {
		return errors.New("use --password-stdin to avoid exposing passwords in process arguments")
	}
	password, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && len(password) == 0 {
		return err
	}
	ph, err := auth.HashPassword(strings.TrimSpace(password))
	if err != nil {
		return err
	}
	_, s, err := open(*cfg)
	if err != nil {
		return err
	}
	defer func() { _ = s.Close() }()
	u, err := s.CreateAdmin(context.Background(), *username, ph)
	if err == nil {
		fmt.Printf("Admin created: %s (%s)\n", u.Username, u.ID)
	}
	return err
}
func tokenCreate(args []string) error {
	f := flag.NewFlagSet("token create", flag.ContinueOnError)
	cfg := f.String("config", "configs/server.yaml", "")
	username := f.String("user", "", "")
	expiry := f.Duration("expires", 24*time.Hour, "")
	max := f.Int("max-uses", 1, "")
	if err := f.Parse(args); err != nil {
		return err
	}
	_, s, err := open(*cfg)
	if err != nil {
		return err
	}
	defer func() { _ = s.Close() }()
	u, _, err := s.Authenticate(context.Background(), *username)
	if err != nil {
		return fmt.Errorf("find user: %w", err)
	}
	t, v, err := s.CreateEnrollmentToken(context.Background(), u.ID, *expiry, *max)
	if err == nil {
		fmt.Printf("Token: %s\nExpires: %s\nMaximum uses: %d\nID: %s\n", v, t.ExpiresAt.Format(time.RFC3339), t.MaxUses, t.ID)
	}
	return err
}
func install(args []string) error {
	f := flag.NewFlagSet("install", flag.ContinueOnError)
	listen := f.String("listen-address", "0.0.0.0", "")
	api := f.Int("api-port", 8443, "")
	tun := f.Int("tunnel-port", 51830, "")
	network := f.String("network", "10.90.0.0/24", "")
	tlsCert := f.String("tls-cert", "", "TLS certificate path")
	tlsKey := f.String("tls-key", "", "TLS private key path")
	if err := f.Parse(args); err != nil {
		return err
	}
	c := config.DefaultServer()
	c.ListenAddress = *listen
	c.APIPort = *api
	c.TunnelPort = *tun
	c.Network = *network
	_, parsedNetwork, err := net.ParseCIDR(c.Network)
	if err != nil {
		return fmt.Errorf("parse network: %w", err)
	}
	serverIP := append(net.IP(nil), parsedNetwork.IP.To4()...)
	for i := len(serverIP) - 1; i >= 0; i-- {
		serverIP[i]++
		if serverIP[i] != 0 {
			break
		}
	}
	c.TunnelAddress = serverIP.String()
	c.TLSCert = *tlsCert
	c.TLSKey = *tlsKey
	c.Database = "/var/lib/tyxnet/tyxnet.db"
	if err := c.Validate(); err != nil {
		return err
	}
	b, _ := yaml.Marshal(c)
	return installer.Install(installer.Spec{Name: "tyxnet-server", Binary: "tyxnet-server", Config: "/etc/tyxnet/server.yaml", ConfigData: b})
}
