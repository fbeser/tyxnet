package routing

import (
	"errors"
	"net"
	"sync"
)

type Peer interface{ Send([]byte) error }
type Router struct {
	mu    sync.RWMutex
	peers map[string]Peer
}

func New() *Router                      { return &Router{peers: make(map[string]Peer)} }
func (r *Router) Add(ip net.IP, p Peer) { r.mu.Lock(); defer r.mu.Unlock(); r.peers[ip.String()] = p }
func (r *Router) Remove(ip net.IP)      { r.mu.Lock(); defer r.mu.Unlock(); delete(r.peers, ip.String()) }
func (r *Router) Route(assigned net.IP, packet []byte) error {
	if len(packet) < 20 || packet[0]>>4 != 4 {
		return errors.New("invalid IPv4 packet")
	}
	src := net.IP(packet[12:16])
	dst := net.IP(packet[16:20])
	if !src.Equal(assigned) {
		return errors.New("source IP spoofing rejected")
	}
	r.mu.RLock()
	p := r.peers[dst.String()]
	r.mu.RUnlock()
	if p == nil {
		return errors.New("unknown destination")
	}
	return p.Send(append([]byte(nil), packet...))
}
