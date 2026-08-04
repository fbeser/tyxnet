package client

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/fbeser/tyxnet/internal/config"
	"github.com/fbeser/tyxnet/internal/storage"
)

type State struct {
	mu            sync.RWMutex
	Connected     bool      `json:"connected"`
	DeviceID      string    `json:"device_id"`
	VirtualIP     string    `json:"virtual_ip"`
	LastConnected time.Time `json:"last_connected"`
	LastError     string    `json:"last_error"`
	Started       time.Time `json:"started"`
}
type persisted struct {
	DeviceID, VirtualIP string
	PrivateKey          []byte
}
type Client struct {
	Config config.Client
	State  *State
	HTTP   *http.Client
	key    ed25519.PrivateKey
}

func New(c config.Client) *Client {
	return &Client{Config: c, State: &State{Started: time.Now()}, HTTP: &http.Client{Timeout: 30 * time.Second}}
}
func (c *Client) Join(ctx context.Context, token string) error {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	in := map[string]any{"Token": token, "Name": c.Config.Name, "OS": runtime.GOOS, "Arch": runtime.GOARCH, "Version": "dev", "public_key": priv.Public().(ed25519.PublicKey)}
	var d storage.Device
	if err = c.request(ctx, "POST", "/api/v1/enroll", in, &d); err != nil {
		return err
	}
	if err = os.MkdirAll(c.Config.StateDir, 0700); err != nil {
		return err
	}
	p := persisted{DeviceID: d.ID, VirtualIP: d.VirtualIP, PrivateKey: priv}
	b, _ := json.Marshal(p)
	if err = os.WriteFile(filepath.Join(c.Config.StateDir, "identity.json"), b, 0600); err != nil {
		return err
	}
	c.key = priv
	c.State.mu.Lock()
	c.State.DeviceID = d.ID
	c.State.VirtualIP = d.VirtualIP
	c.State.mu.Unlock()
	return nil
}
func (c *Client) load() error {
	b, err := os.ReadFile(filepath.Join(c.Config.StateDir, "identity.json"))
	if err != nil {
		return fmt.Errorf("load identity: %w", err)
	}
	var p persisted
	if err = json.Unmarshal(b, &p); err != nil {
		return err
	}
	if len(p.PrivateKey) != ed25519.PrivateKeySize {
		return errors.New("invalid private key")
	}
	c.key = p.PrivateKey
	c.State.mu.Lock()
	c.State.DeviceID = p.DeviceID
	c.State.VirtualIP = p.VirtualIP
	c.State.mu.Unlock()
	return nil
}
func (c *Client) Run(ctx context.Context) error {
	if err := c.load(); err != nil {
		return err
	}
	backoff := []time.Duration{time.Second, 2 * time.Second, 5 * time.Second, 10 * time.Second, 30 * time.Second, time.Minute}
	for i := 0; ; i++ {
		if err := c.connect(ctx); err != nil {
			c.State.mu.Lock()
			c.State.Connected = false
			c.State.LastError = err.Error()
			c.State.mu.Unlock()
		}
		if ctx.Err() != nil {
			return nil
		}
		d := backoff[min(i, len(backoff)-1)]
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(d):
		}
	}
}
func (c *Client) connect(ctx context.Context) error {
	var challenge struct {
		Challenge string `json:"challenge"`
	}
	if err := c.request(ctx, "POST", "/control/v1/challenge", map[string]string{"device_id": c.State.DeviceID}, &challenge); err != nil {
		return err
	}
	nonce, err := base64.RawStdEncoding.DecodeString(challenge.Challenge)
	if err != nil {
		return err
	}
	sig := ed25519.Sign(c.key, nonce)
	body, _ := json.Marshal(map[string]string{"DeviceID": c.State.DeviceID, "Signature": base64.RawStdEncoding.EncodeToString(sig)})
	req, err := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(c.Config.ServerURL, "/")+"/control/v1/connect", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	streamClient := *c.HTTP
	streamClient.Timeout = 0
	resp, err := streamClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("control connect: %s: %s", resp.Status, string(b))
	}
	c.State.mu.Lock()
	c.State.Connected = true
	c.State.LastConnected = time.Now()
	c.State.LastError = ""
	c.State.mu.Unlock()
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		if ctx.Err() != nil {
			return nil
		}
	}
	return scanner.Err()
}
func (c *Client) request(ctx context.Context, method, path string, in, out any) error {
	b, _ := json.Marshal(in)
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.Config.ServerURL, "/")+path, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		x, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("server: %s: %s", resp.Status, string(x))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
func (c *Client) LocalHandler() http.Handler {
	m := http.NewServeMux()
	m.HandleFunc("GET /api/status", func(w http.ResponseWriter, _ *http.Request) {
		c.State.mu.RLock()
		defer c.State.mu.RUnlock()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(c.State)
	})
	m.HandleFunc("GET /", func(w http.ResponseWriter, _ *http.Request) {
		c.State.mu.RLock()
		defer c.State.mu.RUnlock()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, "<!doctype html><meta charset=utf-8><title>TyxNet Client</title><style>body{font:16px system-ui;margin:3rem;background:#101827;color:#e8eef8}</style><h1>TyxNet Client</h1><p>Connected: %t</p><p>Virtual IP: %s</p><p>Last error: %s</p>", c.State.Connected, c.State.VirtualIP, c.State.LastError)
	})
	return m
}
