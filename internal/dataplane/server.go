package dataplane

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	tyxcrypto "github.com/fbeser/tyxnet/internal/crypto"
	"github.com/fbeser/tyxnet/internal/routing"
	"github.com/fbeser/tyxnet/internal/tunnel"
	"github.com/fbeser/tyxnet/pkg/protocol"
)

type Server struct {
	conn      *net.UDPConn
	adapter   tunnel.Device
	serverIP  net.IP
	mtu       int
	router    *routing.Router
	monitor   *routing.TrafficMonitor
	mu        sync.RWMutex
	sessions  map[uint64]*serverSession
	byDevice  map[string]*serverSession
	adapterMu sync.Mutex
	limitMu   sync.Mutex
	invalid   map[string]invalidSource
	closed    chan struct{}
	closeOnce sync.Once
}

type invalidSource struct {
	window time.Time
	count  int
}

type serverSession struct {
	server     *Server
	deviceID   string
	deviceHash uint64
	assignedIP net.IP
	sessionID  uint64
	expiresAt  time.Time
	send       *tyxcrypto.Cipher
	receive    *tyxcrypto.Cipher
	sequence   atomic.Uint64
	addressMu  sync.RWMutex
	address    *net.UDPAddr
}

type adapterPeer struct{ server *Server }

func Listen(address string, adapter tunnel.Device, serverIP net.IP, mtu int, monitor *routing.TrafficMonitor) (*Server, error) {
	if adapter == nil || serverIP.To4() == nil || mtu < 576 || mtu > 9000 || monitor == nil {
		return nil, errors.New("invalid server data-plane configuration")
	}
	udpAddress, err := net.ResolveUDPAddr("udp", address)
	if err != nil {
		return nil, err
	}
	conn, err := net.ListenUDP("udp", udpAddress)
	if err != nil {
		return nil, err
	}
	server := &Server{conn: conn, adapter: adapter, serverIP: append(net.IP(nil), serverIP.To4()...), mtu: mtu, monitor: monitor, sessions: map[uint64]*serverSession{}, byDevice: map[string]*serverSession{}, invalid: map[string]invalidSource{}, closed: make(chan struct{})}
	server.router = routing.NewObserved(monitor.Observe)
	server.router.Add(server.serverIP, adapterPeer{server: server})
	monitor.SetReady(true)
	go server.readUDP()
	go server.readAdapter()
	return server, nil
}

func (s *Server) Port() int { return s.conn.LocalAddr().(*net.UDPAddr).Port }

func (s *Server) Register(deviceID string, assignedIP net.IP) (Bootstrap, error) {
	ip := assignedIP.To4()
	if deviceID == "" || ip == nil || ip.Equal(s.serverIP) {
		return Bootstrap{}, errors.New("invalid data-plane peer")
	}
	bootstrap, keys, sessionID, err := newBootstrap(s.Port())
	if err != nil {
		return Bootstrap{}, err
	}
	send, err := tyxcrypto.NewCipher(keys.serverToClient)
	if err != nil {
		return Bootstrap{}, err
	}
	receive, err := tyxcrypto.NewCipher(keys.clientToServer)
	if err != nil {
		return Bootstrap{}, err
	}
	session := &serverSession{server: s, deviceID: deviceID, deviceHash: deviceHash(deviceID), assignedIP: append(net.IP(nil), ip...), sessionID: sessionID, expiresAt: bootstrap.ExpiresAt, send: send, receive: receive}
	s.mu.Lock()
	if old := s.byDevice[deviceID]; old != nil {
		delete(s.sessions, old.sessionID)
		s.router.Remove(old.assignedIP)
	}
	s.sessions[sessionID] = session
	s.byDevice[deviceID] = session
	s.router.Add(session.assignedIP, session)
	s.mu.Unlock()
	return bootstrap, nil
}

func (s *Server) Remove(deviceID, sessionID string) {
	parsed, _ := parseSessionID(sessionID)
	s.mu.Lock()
	session := s.byDevice[deviceID]
	if session != nil && session.sessionID == parsed {
		delete(s.byDevice, deviceID)
		delete(s.sessions, session.sessionID)
		s.router.Remove(session.assignedIP)
	}
	s.mu.Unlock()
}

func (s *Server) Close() error {
	var err error
	s.closeOnce.Do(func() {
		close(s.closed)
		s.monitor.SetReady(false)
		err = s.conn.Close()
	})
	return err
}

func (s *Server) readUDP() {
	buffer := make([]byte, protocol.HeaderSize+s.mtu+16)
	for {
		n, address, err := s.conn.ReadFromUDP(buffer)
		if err != nil {
			return
		}
		if s.invalidBlocked(address.IP) {
			continue
		}
		encoded := append([]byte(nil), buffer[:n]...)
		packet, err := protocol.ParsePacket(encoded)
		if err != nil || (packet.Type != protocol.TypeData && packet.Type != protocol.TypeKeepalive) {
			s.recordInvalid(address.IP)
			continue
		}
		s.mu.RLock()
		session := s.sessions[packet.SessionID]
		s.mu.RUnlock()
		if session == nil || time.Now().After(session.expiresAt) || packet.SourceID != session.deviceHash {
			s.recordInvalid(address.IP)
			continue
		}
		plaintext, err := openPacket(session.receive, encoded, packet)
		if err != nil {
			s.recordInvalid(address.IP)
			continue
		}
		session.addressMu.Lock()
		session.address = address
		session.addressMu.Unlock()
		if packet.Type == protocol.TypeKeepalive {
			if len(plaintext) != 0 {
				s.recordInvalid(address.IP)
			}
			continue
		}
		if len(plaintext) < 20 || len(plaintext) > s.mtu {
			s.recordInvalid(address.IP)
			continue
		}
		_ = s.router.Route(session.assignedIP, plaintext)
	}
}

func (s *Server) invalidBlocked(ip net.IP) bool {
	now := time.Now()
	key := ip.String()
	s.limitMu.Lock()
	defer s.limitMu.Unlock()
	entry, ok := s.invalid[key]
	if !ok || now.Sub(entry.window) >= time.Minute {
		delete(s.invalid, key)
		return false
	}
	return entry.count >= 120
}

func (s *Server) recordInvalid(ip net.IP) {
	now := time.Now()
	key := ip.String()
	s.limitMu.Lock()
	entry := s.invalid[key]
	if entry.window.IsZero() || now.Sub(entry.window) >= time.Minute {
		entry = invalidSource{window: now}
	}
	entry.count++
	s.invalid[key] = entry
	if len(s.invalid) > 4096 {
		for source, candidate := range s.invalid {
			if now.Sub(candidate.window) >= time.Minute {
				delete(s.invalid, source)
			}
		}
		for source := range s.invalid {
			if len(s.invalid) <= 4096 {
				break
			}
			delete(s.invalid, source)
		}
	}
	s.limitMu.Unlock()
}

func (s *Server) readAdapter() {
	buffer := make([]byte, s.mtu)
	for {
		n, err := s.adapter.Read(buffer)
		if err != nil {
			return
		}
		if n > 0 {
			_ = s.router.Route(s.serverIP, append([]byte(nil), buffer[:n]...))
		}
	}
}

func (p adapterPeer) Send(packet []byte) error {
	p.server.adapterMu.Lock()
	defer p.server.adapterMu.Unlock()
	_, err := p.server.adapter.Write(packet)
	return err
}

func (s *serverSession) Send(plaintext []byte) error {
	s.addressMu.RLock()
	address := s.address
	s.addressMu.RUnlock()
	if address == nil {
		return errors.New("data-plane peer endpoint is unknown")
	}
	sequence := s.sequence.Add(1)
	packet := protocol.Packet{Type: protocol.TypeData, SessionID: s.sessionID, DestinationID: s.deviceHash, Sequence: sequence}
	encoded, err := sealPacket(s.send, packet, plaintext)
	if err != nil {
		return err
	}
	_, err = s.server.conn.WriteToUDP(encoded, address)
	return err
}

func deviceHash(deviceID string) uint64 {
	sum := sha256.Sum256([]byte(deviceID))
	return binary.BigEndian.Uint64(sum[:8])
}

func parseSessionID(value string) (uint64, error) {
	return strconv.ParseUint(value, 10, 64)
}
