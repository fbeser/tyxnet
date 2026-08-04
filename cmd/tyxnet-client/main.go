package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"github.com/fbeser/tyxnet/internal/client"
	"github.com/fbeser/tyxnet/internal/config"
	"github.com/fbeser/tyxnet/internal/installer"
	"gopkg.in/yaml.v3"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
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
func loadArgs(name string, a []string) (config.Client, error) {
	f := flag.NewFlagSet(name, flag.ContinueOnError)
	p := f.String("config", "configs/client.yaml", "")
	if err := f.Parse(a); err != nil {
		return config.Client{}, err
	}
	return config.LoadClient(*p)
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
	c, err := loadArgs("run", a)
	if err != nil {
		return err
	}
	cl := client.New(c)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	srv := &http.Server{Addr: c.LocalAddress, Handler: cl.LocalHandler(), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		x, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(x)
	}()
	go srv.ListenAndServe()
	return cl.Run(ctx)
}
func install(a []string) error {
	f := flag.NewFlagSet("install", flag.ContinueOnError)
	server := f.String("server", "", "")
	token := f.String("token", "", "")
	name := f.String("name", "", "")
	if err := f.Parse(a); err != nil {
		return err
	}
	c := config.DefaultClient()
	c.ServerURL = *server
	c.Name = *name
	c.StateDir = "/var/lib/tyxnet/client"
	if err := c.Validate(); err != nil {
		return err
	}
	if err := client.New(c).Join(context.Background(), *token); err != nil {
		return err
	}
	b, _ := yaml.Marshal(c)
	return installer.Install(installer.Spec{Name: "tyxnet-client", Binary: "tyxnet-client", Config: "/etc/tyxnet/client.yaml", ConfigData: b})
}
