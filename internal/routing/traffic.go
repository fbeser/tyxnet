package routing

import (
	"encoding/binary"
	"net"
	"sort"
	"sync"
	"time"
)

const (
	trafficWindowSeconds = 60
	rateWindowSeconds    = 5
	maxTrackedFlows      = 4096
)

type TrafficFlow struct {
	Source          string  `json:"source"`
	Destination     string  `json:"destination"`
	Protocol        string  `json:"protocol"`
	ProtocolNumber  uint8   `json:"protocol_number"`
	SourcePort      *uint16 `json:"source_port,omitempty"`
	DestinationPort *uint16 `json:"destination_port,omitempty"`
	ICMPType        *uint8  `json:"icmp_type,omitempty"`
	ICMPCode        *uint8  `json:"icmp_code,omitempty"`
	Bytes           uint64  `json:"bytes"`
	Packets         uint64  `json:"packets"`
	Mbps            float64 `json:"mbps"`
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
	source          string
	destination     string
	protocol        string
	protocolNumber  uint8
	hasPorts        bool
	sourcePort      uint16
	destinationPort uint16
	hasICMP         bool
	icmpType        uint8
	icmpCode        uint8
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
	keys    map[trafficKey]int
}

func NewTrafficMonitor() *TrafficMonitor {
	return &TrafficMonitor{now: time.Now, buckets: make(map[int64]map[trafficKey]trafficCount), keys: make(map[trafficKey]int)}
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
	m.observe(trafficKey{source: source.String(), destination: destination.String(), protocol: "other"}, packetBytes)
}

// ObservePacket records only IPv4 and transport-header metadata. Packet payloads
// and DNS/application content are neither copied nor retained.
func (m *TrafficMonitor) ObservePacket(packet []byte) {
	key, ok := trafficKeyFromIPv4(packet)
	if !ok {
		return
	}
	m.observe(key, len(packet))
}

func (m *TrafficMonitor) observe(key trafficKey, packetBytes int) {
	if packetBytes <= 0 || key.source == "" || key.destination == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	second := m.now().UTC().Unix()
	m.prune(second)
	if m.buckets[second] == nil {
		m.buckets[second] = make(map[trafficKey]trafficCount)
	}
	if _, exists := m.buckets[second][key]; !exists {
		if m.keys[key] == 0 && len(m.keys) >= maxTrackedFlows {
			return
		}
		m.keys[key]++
	}
	count := m.buckets[second][key]
	count.bytes += uint64(packetBytes)
	count.packets++
	m.buckets[second][key] = count
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
		flow := TrafficFlow{Source: key.source, Destination: key.destination, Protocol: key.protocol, ProtocolNumber: key.protocolNumber, Bytes: count.bytes, Packets: count.packets, Mbps: megabitsPerSecond(rateBytes[key], rateWindowSeconds)}
		if key.hasPorts {
			sourcePort, destinationPort := key.sourcePort, key.destinationPort
			flow.SourcePort, flow.DestinationPort = &sourcePort, &destinationPort
		}
		if key.hasICMP {
			icmpType, icmpCode := key.icmpType, key.icmpCode
			flow.ICMPType, flow.ICMPCode = &icmpType, &icmpCode
		}
		snapshot.Flows = append(snapshot.Flows, flow)
	}
	sort.Slice(snapshot.Flows, func(i, j int) bool {
		left, right := snapshot.Flows[i], snapshot.Flows[j]
		if left.Source != right.Source {
			return left.Source < right.Source
		}
		if left.Destination != right.Destination {
			return left.Destination < right.Destination
		}
		if left.ProtocolNumber != right.ProtocolNumber {
			return left.ProtocolNumber < right.ProtocolNumber
		}
		if (left.SourcePort == nil) != (right.SourcePort == nil) {
			return left.SourcePort == nil
		}
		if portValue(left.SourcePort) != portValue(right.SourcePort) {
			return portValue(left.SourcePort) < portValue(right.SourcePort)
		}
		if (left.DestinationPort == nil) != (right.DestinationPort == nil) {
			return left.DestinationPort == nil
		}
		if portValue(left.DestinationPort) != portValue(right.DestinationPort) {
			return portValue(left.DestinationPort) < portValue(right.DestinationPort)
		}
		if (left.ICMPType == nil) != (right.ICMPType == nil) {
			return left.ICMPType == nil
		}
		if byteValue(left.ICMPType) != byteValue(right.ICMPType) {
			return byteValue(left.ICMPType) < byteValue(right.ICMPType)
		}
		if (left.ICMPCode == nil) != (right.ICMPCode == nil) {
			return left.ICMPCode == nil
		}
		return byteValue(left.ICMPCode) < byteValue(right.ICMPCode)
	})
	for _, bytes := range rateBytes {
		snapshot.Mbps += megabitsPerSecond(bytes, rateWindowSeconds)
	}
	return snapshot
}

func trafficKeyFromIPv4(packet []byte) (trafficKey, bool) {
	if len(packet) < 20 || packet[0]>>4 != 4 {
		return trafficKey{}, false
	}
	headerLength := int(packet[0]&0x0f) * 4
	if headerLength < 20 || headerLength > len(packet) {
		return trafficKey{}, false
	}
	protocolNumber := packet[9]
	key := trafficKey{source: net.IP(packet[12:16]).String(), destination: net.IP(packet[16:20]).String(), protocol: protocolName(protocolNumber), protocolNumber: protocolNumber}
	fragmentOffset := binary.BigEndian.Uint16(packet[6:8]) & 0x1fff
	if fragmentOffset != 0 {
		return key, true
	}
	switch protocolNumber {
	case 6, 17:
		if len(packet) >= headerLength+4 {
			key.hasPorts = true
			key.sourcePort = binary.BigEndian.Uint16(packet[headerLength : headerLength+2])
			key.destinationPort = binary.BigEndian.Uint16(packet[headerLength+2 : headerLength+4])
		}
	case 1:
		if len(packet) >= headerLength+2 {
			key.hasICMP = true
			key.icmpType = packet[headerLength]
			key.icmpCode = packet[headerLength+1]
		}
	}
	return key, true
}

func protocolName(number uint8) string {
	switch number {
	case 1:
		return "icmp"
	case 6:
		return "tcp"
	case 17:
		return "udp"
	default:
		return "other"
	}
}

func portValue(port *uint16) uint16 {
	if port == nil {
		return 0
	}
	return *port
}

func byteValue(value *uint8) uint8 {
	if value == nil {
		return 0
	}
	return *value
}

func (m *TrafficMonitor) prune(currentSecond int64) {
	for second := range m.buckets {
		if second <= currentSecond-trafficWindowSeconds {
			for key := range m.buckets[second] {
				m.keys[key]--
				if m.keys[key] <= 0 {
					delete(m.keys, key)
				}
			}
			delete(m.buckets, second)
		}
	}
}

func megabitsPerSecond(bytes uint64, seconds int) float64 {
	return float64(bytes*8) / float64(seconds) / 1_000_000
}
