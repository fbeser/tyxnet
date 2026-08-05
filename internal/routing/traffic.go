package routing

import (
	"net"
	"sort"
	"sync"
	"time"
)

const (
	trafficWindowSeconds = 60
	rateWindowSeconds    = 5
)

type TrafficFlow struct {
	Source      string  `json:"source"`
	Destination string  `json:"destination"`
	Bytes       uint64  `json:"bytes"`
	Packets     uint64  `json:"packets"`
	Mbps        float64 `json:"mbps"`
}

type TrafficPoint struct {
	Timestamp time.Time `json:"timestamp"`
	Bytes     uint64    `json:"bytes"`
	Packets   uint64    `json:"packets"`
	Mbps      float64   `json:"mbps"`
}

type TrafficSnapshot struct {
	GeneratedAt       time.Time      `json:"generated_at"`
	WindowSeconds     int            `json:"window_seconds"`
	RateWindowSeconds int            `json:"rate_window_seconds"`
	Ready             bool           `json:"data_plane_ready"`
	Bytes             uint64         `json:"bytes"`
	Packets           uint64         `json:"packets"`
	Mbps              float64        `json:"mbps"`
	Flows             []TrafficFlow  `json:"flows"`
	Series            []TrafficPoint `json:"series"`
}

type trafficKey struct {
	source      string
	destination string
}

type trafficCount struct {
	bytes   uint64
	packets uint64
}

type TrafficMonitor struct {
	mu      sync.Mutex
	now     func() time.Time
	ready   bool
	buckets map[int64]map[trafficKey]trafficCount
}

func NewTrafficMonitor() *TrafficMonitor {
	return &TrafficMonitor{now: time.Now, buckets: make(map[int64]map[trafficKey]trafficCount)}
}

func (m *TrafficMonitor) SetReady(ready bool) {
	m.mu.Lock()
	m.ready = ready
	m.mu.Unlock()
}

func (m *TrafficMonitor) Observe(source, destination net.IP, packetBytes int) {
	if packetBytes <= 0 || source == nil || destination == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	second := m.now().UTC().Unix()
	if m.buckets[second] == nil {
		m.buckets[second] = make(map[trafficKey]trafficCount)
	}
	key := trafficKey{source: source.String(), destination: destination.String()}
	count := m.buckets[second][key]
	count.bytes += uint64(packetBytes)
	count.packets++
	m.buckets[second][key] = count
	m.prune(second)
}

func (m *TrafficMonitor) Snapshot() TrafficSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now().UTC().Truncate(time.Second)
	currentSecond := now.Unix()
	m.prune(currentSecond)
	flows := make(map[trafficKey]trafficCount)
	rateBytes := make(map[trafficKey]uint64)
	series := make([]TrafficPoint, 0, trafficWindowSeconds)
	snapshot := TrafficSnapshot{GeneratedAt: now, WindowSeconds: trafficWindowSeconds, RateWindowSeconds: rateWindowSeconds, Ready: m.ready, Flows: make([]TrafficFlow, 0)}
	for offset := trafficWindowSeconds - 1; offset >= 0; offset-- {
		second := currentSecond - int64(offset)
		point := TrafficPoint{Timestamp: time.Unix(second, 0).UTC()}
		for key, count := range m.buckets[second] {
			flow := flows[key]
			flow.bytes += count.bytes
			flow.packets += count.packets
			flows[key] = flow
			point.Bytes += count.bytes
			point.Packets += count.packets
			if second > currentSecond-rateWindowSeconds {
				rateBytes[key] += count.bytes
			}
		}
		point.Mbps = megabitsPerSecond(point.Bytes, 1)
		snapshot.Bytes += point.Bytes
		snapshot.Packets += point.Packets
		series = append(series, point)
	}
	snapshot.Series = series
	for key, count := range flows {
		snapshot.Flows = append(snapshot.Flows, TrafficFlow{Source: key.source, Destination: key.destination, Bytes: count.bytes, Packets: count.packets, Mbps: megabitsPerSecond(rateBytes[key], rateWindowSeconds)})
	}
	sort.Slice(snapshot.Flows, func(i, j int) bool {
		if snapshot.Flows[i].Mbps == snapshot.Flows[j].Mbps {
			return snapshot.Flows[i].Bytes > snapshot.Flows[j].Bytes
		}
		return snapshot.Flows[i].Mbps > snapshot.Flows[j].Mbps
	})
	for _, bytes := range rateBytes {
		snapshot.Mbps += megabitsPerSecond(bytes, rateWindowSeconds)
	}
	return snapshot
}

func (m *TrafficMonitor) prune(currentSecond int64) {
	for second := range m.buckets {
		if second <= currentSecond-trafficWindowSeconds {
			delete(m.buckets, second)
		}
	}
}

func megabitsPerSecond(bytes uint64, seconds int) float64 {
	return float64(bytes*8) / float64(seconds) / 1_000_000
}
