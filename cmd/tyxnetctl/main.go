package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/fbeser/tyxnet/internal/apiclient"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
func base(a []string) (apiclient.Client, []string, error) {
	f := flag.NewFlagSet("tyxnetctl", flag.ContinueOnError)
	server := f.String("server", os.Getenv("TYXNET_SERVER"), "")
	token := f.String("token", os.Getenv("TYXNET_ACCESS_TOKEN"), "")
	if err := f.Parse(a); err != nil {
		return apiclient.Client{}, nil, err
	}
	if *server == "" {
		return apiclient.Client{}, nil, errors.New("--server or TYXNET_SERVER is required")
	}
	return apiclient.Client{BaseURL: *server, Token: *token}, f.Args(), nil
}
func run(a []string) error {
	c, args, err := base(a)
	if err != nil {
		return err
	}
	if len(args) == 0 {
		return errors.New("usage: tyxnetctl [--server URL] <login|devices|users|tokens>")
	}
	ctx := context.Background()
	var out any
	switch args[0] {
	case "login":
		f := flag.NewFlagSet("login", flag.ContinueOnError)
		u := f.String("username", "admin", "")
		p := f.String("password", "", "")
		if err = f.Parse(args[1:]); err != nil {
			return err
		}
		var v map[string]any
		err = c.Do(ctx, "POST", "/api/v1/auth/login", map[string]string{"username": *u, "password": *p}, &v)
		out = v
	case "devices":
		if len(args) < 2 {
			return errors.New("devices subcommand required")
		}
		switch {
		case args[1] == "list":
			var v []map[string]any
			err = c.Do(ctx, "GET", "/api/v1/devices", nil, &v)
			out = v
		case len(args) == 3 && (args[1] == "restart" || args[1] == "shutdown" || args[1] == "disconnect"):
			var v map[string]any
			err = c.Do(ctx, "POST", "/api/v1/devices/"+args[2]+"/"+args[1], nil, &v)
			out = v
		case len(args) == 3 && args[1] == "revoke":
			err = c.Do(ctx, "DELETE", "/api/v1/devices/"+args[2], nil, nil)
			out = map[string]string{"status": "revoked"}
		default:
			return errors.New("usage: devices <list|restart ID|shutdown ID|disconnect ID|revoke ID>")
		}
	case "users":
		if len(args) < 2 || args[1] == "list" {
			var v []map[string]any
			err = c.Do(ctx, "GET", "/api/v1/users", nil, &v)
			out = v
		} else if args[1] == "create" {
			f := flag.NewFlagSet("users create", flag.ContinueOnError)
			name := f.String("username", "", "")
			password := f.String("password", "", "")
			role := f.String("role", "member", "")
			if err = f.Parse(args[2:]); err != nil {
				return err
			}
			var v map[string]any
			err = c.Do(ctx, "POST", "/api/v1/users", map[string]string{"Username": *name, "Password": *password, "Role": *role}, &v)
			out = v
		} else if args[1] == "delete" && len(args) == 3 {
			err = c.Do(ctx, "DELETE", "/api/v1/users/"+args[2], nil, nil)
			out = map[string]string{"status": "deleted"}
		} else {
			return errors.New("usage: users <list|create|delete ID>")
		}
	case "tokens":
		if len(args) < 2 {
			return errors.New("tokens subcommand required")
		}
		switch args[1] {
		case "list":
			var v []map[string]any
			err = c.Do(ctx, "GET", "/api/v1/tokens", nil, &v)
			out = v
		case "create":
			f := flag.NewFlagSet("tokens create", flag.ContinueOnError)
			uid := f.String("user-id", "", "")
			exp := f.Duration("expires", 24*time.Hour, "")
			max := f.Int("max-uses", 1, "")
			if err = f.Parse(args[2:]); err != nil {
				return err
			}
			var v map[string]any
			err = c.Do(ctx, "POST", "/api/v1/tokens", map[string]any{"user_id": *uid, "expires": exp.String(), "max_uses": *max}, &v)
			out = v
		case "revoke":
			if len(args) != 3 {
				return errors.New("usage: tokens revoke <token-id>")
			}
			err = c.Do(ctx, "DELETE", "/api/v1/tokens/"+args[2], nil, nil)
			out = map[string]string{"status": "revoked"}
		default:
			return errors.New("unsupported tokens subcommand")
		}
	default:
		return errors.New("unsupported command")
	}
	if err != nil {
		return err
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println(string(b))
	return nil
}
