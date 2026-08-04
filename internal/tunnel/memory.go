package tunnel

import (
	"errors"
	"sync"
)

type Memory struct {
	name    string
	mu      sync.Mutex
	packets [][]byte
	closed  bool
}

func NewMemory(name string) *Memory { return &Memory{name: name} }
func (m *Memory) Name() string      { return m.name }
func (m *Memory) Write(b []byte) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return 0, errors.New("closed")
	}
	m.packets = append(m.packets, append([]byte(nil), b...))
	return len(b), nil
}
func (m *Memory) Read(b []byte) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.packets) == 0 {
		return 0, errors.New("no packet")
	}
	n := copy(b, m.packets[0])
	m.packets = m.packets[1:]
	return n, nil
}
func (m *Memory) Close() error { m.mu.Lock(); defer m.mu.Unlock(); m.closed = true; return nil }
