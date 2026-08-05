package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Server struct {
	ListenAddress     string        `yaml:"listen_address"`
	APIPort           int           `yaml:"api_port"`
	TunnelPort        int           `yaml:"tunnel_port"`
	TunnelEnabled     bool          `yaml:"tunnel_enabled"`
	TunnelName        string        `yaml:"tunnel_name"`
	TunnelAddress     string        `yaml:"tunnel_address"`
	TunnelMTU         int           `yaml:"tunnel_mtu"`
	Network           string        `yaml:"network"`
	Database          string        `yaml:"database"`
	TLSCert           string        `yaml:"tls_cert"`
	TLSKey            string        `yaml:"tls_key"`
	AllowInsecureHTTP bool          `yaml:"allow_insecure_http"`
	AllowRemoteSetup  bool          `yaml:"allow_remote_setup"`
	SessionTTL        time.Duration `yaml:"session_ttl"`
	PingInterval      time.Duration `yaml:"ping_interval"`
}

type Client struct {
	ServerURL      string        `yaml:"server"`
	TunnelEndpoint string        `yaml:"tunnel_endpoint"`
	Name           string        `yaml:"name"`
	StateDir       string        `yaml:"state_dir"`
	LocalAddress   string        `yaml:"local_address"`
	AllowRemoteUI  bool          `yaml:"allow_remote_ui"`
	Keepalive      time.Duration `yaml:"keepalive"`
	TunnelEnabled  bool          `yaml:"tunnel_enabled"`
	TunnelName     string        `yaml:"tunnel_name"`
	TunnelMTU      int           `yaml:"tunnel_mtu"`
}

func DefaultServer() Server {
	return Server{ListenAddress: "0.0.0.0", APIPort: 8443, TunnelPort: 51830, TunnelEnabled: true, TunnelName: "TyxNet", TunnelAddress: "10.90.0.1", TunnelMTU: 1280, Network: "10.90.0.0/24", Database: "tyxnet.db", AllowInsecureHTTP: true, AllowRemoteSetup: true, SessionTTL: 15 * time.Minute, PingInterval: 25 * time.Second}
}
func DefaultClient() Client {
	return Client{StateDir: "./client-state", LocalAddress: "0.0.0.0:9070", AllowRemoteUI: true, Keepalive: 25 * time.Second, TunnelEnabled: true, TunnelMTU: 1280}
}

func LoadServer(path string) (Server, error) {
	c := DefaultServer()
	if err := load(path, &c); err != nil {
		return c, err
	}
	return c, c.Validate()
}
func LoadClient(path string) (Client, error) {
	c := DefaultClient()
	if err := load(path, &c); err != nil {
		return c, err
	}
	return c, c.Validate()
}

func SaveClient(path string, c Client) error {
	if err := c.Validate(); err != nil {
		return err
	}
	b, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("encode client config: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil && filepath.Dir(path) != "." {
		return fmt.Errorf("create config directory: %w", err)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return fmt.Errorf("write client config: %w", err)
	}
	return nil
}

func load(path string, out any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}
	if err := yaml.Unmarshal(b, out); err != nil {
		return fmt.Errorf("decode config: %w", err)
	}
	return nil
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
	if c.PingInterval < 5*time.Second || c.PingInterval > time.Hour {
		return errors.New("ping_interval must be between 5s and 1h")
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
	if c.TunnelEnabled {
		if strings.TrimSpace(c.TunnelName) == "" || len(c.TunnelName) > 15 {
			return errors.New("tunnel_name must be between 1 and 15 characters")
		}
		address := net.ParseIP(c.TunnelAddress)
		if address == nil || address.To4() == nil || !n.Contains(address) {
			return errors.New("tunnel_address must be an IPv4 address inside network")
		}
		if address.Equal(n.IP) || isIPv4Broadcast(address, n) {
			return errors.New("tunnel_address cannot be the network or broadcast address")
		}
		if c.TunnelMTU < 576 || c.TunnelMTU > 9000 {
			return errors.New("tunnel_mtu must be between 576 and 9000")
		}
	}
	if (c.TLSCert == "") != (c.TLSKey == "") {
		return errors.New("tls_cert and tls_key must be configured together")
	}
	if c.ListenAddress != "127.0.0.1" && c.ListenAddress != "::1" && c.TLSCert == "" && !c.AllowInsecureHTTP {
		return errors.New("non-loopback HTTP requires TLS or explicit allow_insecure_http")
	}
	return nil
}

func isIPv4Broadcast(ip net.IP, n *net.IPNet) bool {
	base := n.IP.To4()
	v := ip.To4()
	if base == nil || v == nil {
		return false
	}
	for i := 0; i < 4; i++ {
		if v[i] != base[i]|^n.Mask[i] {
			return false
		}
	}
	return true
}

func (c Client) Validate() error {
	serverURL := strings.TrimSpace(c.ServerURL)
	name := strings.TrimSpace(c.Name)
	if (serverURL == "") != (name == "") {
		return errors.New("server and name must either both be set or both be empty")
	}
	if serverURL != "" {
		u, err := url.Parse(serverURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return errors.New("server must be an http or https URL")
		}
	}
	host, _, err := net.SplitHostPort(c.LocalAddress)
	if err != nil || net.ParseIP(host) == nil {
		return errors.New("local_address must contain an IP address and port")
	}
	if host != "127.0.0.1" && host != "::1" && !c.AllowRemoteUI {
		return errors.New("non-loopback local_address requires explicit allow_remote_ui")
	}
	if c.Keepalive < 5*time.Second {
		return errors.New("keepalive must be at least 5s")
	}
	if c.TunnelEnabled {
		if c.TunnelEndpoint != "" {
			host, port, splitErr := net.SplitHostPort(c.TunnelEndpoint)
			if splitErr != nil || strings.TrimSpace(host) == "" {
				return errors.New("tunnel_endpoint must use host:port format")
			}
			portNumber, parseErr := strconv.Atoi(port)
			if parseErr != nil || portNumber < 1 || portNumber > 65535 {
				return errors.New("tunnel_endpoint port must be between 1 and 65535")
			}
		}
		if len(c.TunnelName) > 15 {
			return errors.New("tunnel_name must be at most 15 characters")
		}
		if c.TunnelMTU < 576 || c.TunnelMTU > 9000 {
			return errors.New("tunnel_mtu must be between 576 and 9000")
		}
	}
	return nil
}
