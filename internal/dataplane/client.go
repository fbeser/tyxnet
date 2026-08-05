package dataplane

import (
	"errors"
	"net"
	"net/url"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	tyxcrypto "github.com/fbeser/tyxnet/internal/crypto"
	"github.com/fbeser/tyxnet/internal/tunnel"
	"github.com/fbeser/tyxnet/pkg/protocol"
)

type Client struct {
	adapter    tunnel.Device
	assignedIP net.IP
	deviceHash uint64
	mtu        int
	conn       *net.UDPConn
	mu         sync.RWMutex
	session    *clientSession
	closed     chan struct{}
	closeOnce  sync.Once
}

type clientSession struct {
	sessionID uint64
	expiresAt time.Time
	endpoint  *net.UDPAddr
	send      *tyxcrypto.Cipher
	receive   *tyxcrypto.Cipher
	sequence  atomic.Uint64
}

func NewClient(adapter tunnel.Device, assignedIP net.IP, deviceID string, mtu int) (*Client, error) {
	if adapter == nil || assignedIP.To4() == nil || deviceID == "" || mtu < 576 || mtu > 9000 {
		return nil, errors.New("invalid client data-plane configuration")
	}
	conn, err := net.ListenUDP("udp", nil)
	if err != nil {
		return nil, err
	}
	client := &Client{adapter: adapter, assignedIP: append(net.IP(nil), assignedIP.To4()...), deviceHash: deviceHash(deviceID), mtu: mtu, conn: conn, closed: make(chan struct{})}
	go client.readAdapter()
	go client.readUDP()
	go client.keepalive()
	return client, nil
}

func (c *Client) Configure(serverURL, endpointOverride string, bootstrap Bootstrap) error {
	keys, sessionID, err := parseBootstrap(bootstrap)
	if err != nil {
		return err
	}
	host := endpointOverride
	if host == "" {
		parsed, parseErr := url.Parse(serverURL)
		if parseErr != nil || parsed.Hostname() == "" {
			return errors.New("invalid data-plane server URL")
		}
		host = net.JoinHostPort(parsed.Hostname(), strconv.Itoa(bootstrap.Port))
	}
	endpoint, err := net.ResolveUDPAddr("udp", host)
	if err != nil {
		return err
	}
	send, err := tyxcrypto.NewCipher(keys.clientToServer)
	if err != nil {
		return err
	}
	receive, err := tyxcrypto.NewCipher(keys.serverToClient)
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.session = &clientSession{sessionID: sessionID, expiresAt: bootstrap.ExpiresAt, endpoint: endpoint, send: send, receive: receive}
	c.mu.Unlock()
	return c.send(protocol.TypeKeepalive, nil)
}

func (c *Client) Close() error {
	var err error
	c.closeOnce.Do(func() {
		close(c.closed)
		err = c.conn.Close()
	})
	return err
}

func (c *Client) readAdapter() {
	buffer := make([]byte, c.mtu)
	for {
		n, err := c.adapter.Read(buffer)
		if err != nil {
			return
		}
		if n > 0 {
			_ = c.send(protocol.TypeData, append([]byte(nil), buffer[:n]...))
		}
	}
}

func (c *Client) readUDP() {
	buffer := make([]byte, protocol.HeaderSize+c.mtu+16)
	for {
		n, source, err := c.conn.ReadFromUDP(buffer)
		if err != nil {
			return
		}
		encoded := append([]byte(nil), buffer[:n]...)
		packet, err := protocol.ParsePacket(encoded)
		if err != nil || packet.Type != protocol.TypeData {
			continue
		}
		c.mu.RLock()
		session := c.session
		c.mu.RUnlock()
		if session == nil || packet.SessionID != session.sessionID || packet.DestinationID != c.deviceHash || !source.IP.Equal(session.endpoint.IP) || source.Port != session.endpoint.Port {
			continue
		}
		plaintext, err := openPacket(session.receive, encoded, packet)
		if err != nil || len(plaintext) < 20 || len(plaintext) > c.mtu || !net.IP(plaintext[16:20]).Equal(c.assignedIP) {
			continue
		}
		_, _ = c.adapter.Write(plaintext)
	}
}

func (c *Client) keepalive() {
	ticker := time.NewTicker(keepalivePeriod)
	defer ticker.Stop()
	for {
		select {
		case <-c.closed:
			return
		case <-ticker.C:
			_ = c.send(protocol.TypeKeepalive, nil)
		}
	}
}

func (c *Client) send(packetType protocol.Type, plaintext []byte) error {
	c.mu.RLock()
	session := c.session
	c.mu.RUnlock()
	if session == nil || time.Now().After(session.expiresAt) {
		return errors.New("data-plane session is unavailable")
	}
	sequence := session.sequence.Add(1)
	packet := protocol.Packet{Type: packetType, SessionID: session.sessionID, SourceID: c.deviceHash, Sequence: sequence}
	encoded, err := sealPacket(session.send, packet, plaintext)
	if err != nil {
		return err
	}
	_, err = c.conn.WriteToUDP(encoded, session.endpoint)
	return err
}
