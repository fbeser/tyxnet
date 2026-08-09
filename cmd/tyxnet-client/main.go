package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/fbeser/tyxnet/internal/application"
	"github.com/fbeser/tyxnet/internal/client"
	"github.com/fbeser/tyxnet/internal/config"
	"github.com/fbeser/tyxnet/internal/installer"
	"gopkg.in/yaml.v3"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
func run(a []string) error {
	if len(a) == 0 {
		return errors.New("usage: tyxnet-client <run|join|install|uninstall|start|stop|restart|status|logs|connect|disconnect>")
	}
	switch a[0] {
	case "run", "connect":
		return serve(a[1:])
	case "join":
		return join(a[1:])
	case "install":
		return install(a[1:])
	case "uninstall":
		return installer.Uninstall("tyxnet-client", "tyxnet-client", "/etc/tyxnet/client.yaml")
	case "start", "stop", "restart", "status", "logs":
		return installer.ServiceAction("tyxnet-client", a[0])
	case "disconnect":
		return installer.ServiceAction("tyxnet-client", "stop")
	}
	return errors.New("unknown command")
}
func runFlags(a []string) (config.Client, string, error) {
	f := flag.NewFlagSet("run", flag.ContinueOnError)
	p := f.String("config", "configs/client.yaml", "")
	localWeb := f.Bool("local-web", false, "restrict the setup and status UI to this machine")
	lanWeb := f.Bool("lan-web", false, "explicitly expose the UI on the LAN (deprecated; this is the default)")
	if err := f.Parse(a); err != nil {
		return config.Client{}, "", err
	}
	if *localWeb && *lanWeb {
		return config.Client{}, "", errors.New("--local-web and --lan-web cannot be used together")
	}
	c, err := config.LoadClient(*p)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return config.Client{}, "", err
		}
		c = config.DefaultClient()
		c.ServerURL = "https://setup.invalid"
		c.Name = "unconfigured"
	}
	if *localWeb {
		c.LocalAddress = "127.0.0.1:9070"
		c.AllowRemoteUI = false
	} else if *lanWeb {
		c.LocalAddress = "0.0.0.0:9070"
		c.AllowRemoteUI = true
	}
	if err := c.Validate(); err != nil {
		return config.Client{}, "", err
	}
	return c, *p, nil
}
func join(a []string) error {
	f := flag.NewFlagSet("join", flag.ContinueOnError)
	server := f.String("server", "", "")
	token := f.String("token", "", "")
	name := f.String("name", "", "")
	state := f.String("state-dir", "./client-state", "")
	if err := f.Parse(a); err != nil {
		return err
	}
	c := config.DefaultClient()
	c.ServerURL = *server
	c.Name = *name
	c.StateDir = *state
	if err := c.Validate(); err != nil {
		return err
	}
	cl := client.New(c)
	if err := cl.Join(context.Background(), *token); err != nil {
		return err
	}
	fmt.Println("Device joined; identity saved with owner-only permissions.")
	return nil
}
func serve(a []string) error {
	c, configPath, err := runFlags(a)
	if err != nil {
		return err
	}
	cl := client.New(c)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	executable, _ := os.Executable()
	executable, _ = filepath.Abs(executable)
	workingDirectory, _ := os.Getwd()
	configPath, _ = filepath.Abs(configPath)
	companion := filepath.Join(filepath.Dir(executable), "tyxnet-tray")
	if runtime.GOOS == "windows" {
		companion += ".exe"
	} else if runtime.GOOS != "darwin" {
		companion = ""
	}
	trayToken := application.TrayToken()
	startupArgs := []string{"run", "--config", configPath}
	host, _, _ := net.SplitHostPort(c.LocalAddress)
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		startupArgs = append(startupArgs, "--local-web")
	}
	localURL := "http://127.0.0.1:9070"
	if _, port, splitErr := net.SplitHostPort(c.LocalAddress); splitErr == nil {
		localURL = "http://127.0.0.1:" + port
	}
	cl.ConfigureApplication(application.StartupSpec{ID: "tyxnet-client", DisplayName: "TyxNet Client", Executable: executable, WorkingDirectory: workingDirectory, Arguments: startupArgs, Companion: companion, CompanionArgs: []string{"--client-url", localURL}, TrayToken: trayToken}, trayToken, stop)
	srv := &http.Server{Addr: c.LocalAddress, Handler: cl.LocalHandler(configPath), ReadHeaderTimeout: 5 * time.Second}
	errCh := make(chan error, 2)
	go func() {
		<-ctx.Done()
		x, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(x)
	}()
	go func() {
		if listenErr := srv.ListenAndServe(); !errors.Is(listenErr, http.ErrServerClosed) {
			errCh <- listenErr
		}
	}()
	go func() { errCh <- cl.Run(ctx) }()
	return <-errCh
}
func install(a []string) error {
	f := flag.NewFlagSet("install", flag.ContinueOnError)
	server := f.String("server", "", "")
	token := f.String("token", "", "")
	name := f.String("name", "", "")
	if err := f.Parse(a); err != nil {
		return err
	}
	c, configured, err := installConfiguration(*server, *token, *name)
	if err != nil {
		return err
	}
	if configured {
		if err := client.New(c).Join(context.Background(), *token); err != nil {
			return err
		}
	}
	b, _ := yaml.Marshal(c)
	return installer.Install(installer.Spec{Name: "tyxnet-client", Binary: "tyxnet-client", Config: "/etc/tyxnet/client.yaml", ConfigData: b})
}

func installConfiguration(server, token, name string) (config.Client, bool, error) {
	c := config.DefaultClient()
	c.ServerURL = server
	c.Name = name
	c.StateDir = "/var/lib/tyxnet/client"
	configured := server != "" || token != "" || name != ""
	if configured && (server == "" || token == "" || name == "") {
		return c, false, errors.New("--server, --token, and --name must be supplied together")
	}
	if err := c.Validate(); err != nil {
		return c, false, err
	}
	return c, configured, nil
}
