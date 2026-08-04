package config

import (
	"errors"
	"fmt"
	"net"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Server struct {
	ListenAddress     string        `yaml:"listen_address"`
	APIPort           int           `yaml:"api_port"`
	TunnelPort        int           `yaml:"tunnel_port"`
	Network           string        `yaml:"network"`
	Database          string        `yaml:"database"`
	TLSCert           string        `yaml:"tls_cert"`
	TLSKey            string        `yaml:"tls_key"`
	AllowInsecureHTTP bool          `yaml:"allow_insecure_http"`
	SessionTTL        time.Duration `yaml:"session_ttl"`
}

type Client struct {
	ServerURL    string        `yaml:"server"`
	Name         string        `yaml:"name"`
	StateDir     string        `yaml:"state_dir"`
	LocalAddress string        `yaml:"local_address"`
	Keepalive    time.Duration `yaml:"keepalive"`
}

func DefaultServer() Server {
	return Server{ListenAddress: "127.0.0.1", APIPort: 8443, TunnelPort: 51830, Network: "10.90.0.0/24", Database: "tyxnet.db", SessionTTL: 15 * time.Minute}
}
func DefaultClient() Client {
	return Client{StateDir: "./client-state", LocalAddress: "127.0.0.1:9070", Keepalive: 25 * time.Second}
}

func LoadServer(path string) (Server, error) {
	c := DefaultServer()
	return c, load(path, &c, c.Validate)
}
func LoadClient(path string) (Client, error) {
	c := DefaultClient()
	return c, load(path, &c, c.Validate)
}

func load(path string, out any, validate func() error) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}
	if err := yaml.Unmarshal(b, out); err != nil {
		return fmt.Errorf("decode config: %w", err)
	}
	return validate()
}

func (c Server) Validate() error {
	if net.ParseIP(c.ListenAddress) == nil {
		return errors.New("listen_address must be an IP address")
	}
	if c.APIPort < 1 || c.APIPort > 65535 || c.TunnelPort < 1 || c.TunnelPort > 65535 {
		return errors.New("ports must be between 1 and 65535")
	}
	if c.APIPort == c.TunnelPort {
		return errors.New("API and tunnel ports must differ")
	}
	ip, n, err := net.ParseCIDR(c.Network)
	if err != nil || ip.To4() == nil {
		return errors.New("network must be an IPv4 CIDR")
	}
	ones, bits := n.Mask.Size()
	if bits != 32 || ones > 30 {
		return errors.New("network must contain usable client addresses")
	}
	if c.Database == "" {
		return errors.New("database is required")
	}
	if (c.TLSCert == "") != (c.TLSKey == "") {
		return errors.New("tls_cert and tls_key must be configured together")
	}
	if c.ListenAddress != "127.0.0.1" && c.ListenAddress != "::1" && c.TLSCert == "" && !c.AllowInsecureHTTP {
		return errors.New("non-loopback HTTP requires TLS or explicit allow_insecure_http")
	}
	return nil
}

func (c Client) Validate() error {
	if c.ServerURL == "" {
		return errors.New("server is required")
	}
	if c.Name == "" {
		return errors.New("name is required")
	}
	host, _, err := net.SplitHostPort(c.LocalAddress)
	if err != nil || host != "127.0.0.1" {
		return errors.New("local_address must bind to 127.0.0.1")
	}
	if c.Keepalive < 5*time.Second {
		return errors.New("keepalive must be at least 5s")
	}
	return nil
}
