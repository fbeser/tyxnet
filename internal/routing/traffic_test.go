package routing

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"math"
	"net"
	"testing"
	"time"
)

func flowPacket(source, destination net.IP, protocol uint8, sourcePort, destinationPort uint16, size int) []byte {
	packet := make([]byte, size)
	packet[0] = 0x45
	packet[9] = protocol
	copy(packet[12:16], source.To4())
	copy(packet[16:20], destination.To4())
	if size >= 24 {
		binary.BigEndian.PutUint16(packet[20:22], sourcePort)
		binary.BigEndian.PutUint16(packet[22:24], destinationPort)
	}
	return packet
}

func TestTrafficMonitorSnapshotAndExpiry(t *testing.T) {
	current := time.Date(2026, time.August, 5, 10, 0, 0, 0, time.UTC)
	monitor := NewTrafficMonitor()
	monitor.now = func() time.Time { return current }
	monitor.SetReady(true)
	monitor.Observe(net.ParseIP("10.90.0.2"), net.ParseIP("10.90.0.3"), 1_000_000)
	monitor.Observe(net.ParseIP("10.90.0.2"), net.ParseIP("10.90.0.3"), 500_000)

	snapshot := monitor.Snapshot()
	if !snapshot.Ready || snapshot.Bytes != 1_500_000 || snapshot.Packets != 2 || len(snapshot.Flows) != 1 || len(snapshot.Series) != trafficWindowSeconds {
		t.Fatalf("unexpected snapshot: %+v", snapshot)
	}
	if math.Abs(snapshot.Mbps-2.4) > 0.0001 || math.Abs(snapshot.Flows[0].Mbps-2.4) > 0.0001 {
		t.Fatalf("unexpected rate: total=%f flow=%f", snapshot.Mbps, snapshot.Flows[0].Mbps)
	}

	current = current.Add(trafficWindowSeconds * time.Second)
	snapshot = monitor.Snapshot()
	if snapshot.Bytes != 0 || snapshot.Packets != 0 || len(snapshot.Flows) != 0 {
		t.Fatalf("expired traffic remained in snapshot: %+v", snapshot)
	}
}

func TestTrafficMonitorRejectsInvalidObservations(t *testing.T) {
	monitor := NewTrafficMonitor()
	monitor.Observe(nil, net.ParseIP("10.90.0.3"), 100)
	monitor.Observe(net.ParseIP("10.90.0.2"), nil, 100)
	monitor.Observe(net.ParseIP("10.90.0.2"), net.ParseIP("10.90.0.3"), 0)
	if snapshot := monitor.Snapshot(); snapshot.Bytes != 0 || snapshot.Packets != 0 {
		t.Fatalf("invalid traffic was recorded: %+v", snapshot)
	}
}

func TestTrafficMonitorRecordsTransportMetadataInStableOrder(t *testing.T) {
	monitor := NewTrafficMonitor()
	monitor.ObservePacket(flowPacket(net.ParseIP("10.90.0.3"), net.ParseIP("10.90.0.2"), 6, 52000, 443, 80))
	monitor.ObservePacket(flowPacket(net.ParseIP("10.90.0.2"), net.ParseIP("10.90.0.3"), 17, 53000, 53, 60))

	flows := monitor.Snapshot().Flows
	if len(flows) != 2 {
		t.Fatalf("unexpected transport flows: %+v", flows)
	}
	if flows[0].Source != "10.90.0.2" || flows[0].Protocol != "udp" || flows[0].ProtocolNumber != 17 || flows[0].SourcePort == nil || *flows[0].SourcePort != 53000 || flows[0].DestinationPort == nil || *flows[0].DestinationPort != 53 {
		t.Fatalf("unexpected first stable flow: %+v", flows[0])
	}
	if flows[1].Source != "10.90.0.3" || flows[1].Protocol != "tcp" || flows[1].DestinationPort == nil || *flows[1].DestinationPort != 443 {
		t.Fatalf("unexpected second stable flow: %+v", flows[1])
	}
}

func TestTrafficMonitorRecordsICMPWithoutPayload(t *testing.T) {
	packet := flowPacket(net.ParseIP("10.90.0.2"), net.ParseIP("10.90.0.3"), 1, 0, 0, 28)
	packet[20], packet[21] = 8, 0
	monitor := NewTrafficMonitor()
	monitor.ObservePacket(packet)
	flow := monitor.Snapshot().Flows[0]
	if flow.Protocol != "icmp" || flow.ICMPType == nil || *flow.ICMPType != 8 || flow.ICMPCode == nil || *flow.ICMPCode != 0 || flow.SourcePort != nil || flow.DestinationPort != nil {
		t.Fatalf("unexpected ICMP metadata: %+v", flow)
	}
	packet[0] = 0x4f
	monitor.ObservePacket(packet[:20])
	if len(monitor.Snapshot().Flows) != 1 {
		t.Fatal("malformed IPv4 packet was recorded")
	}
}

func TestTrafficMonitorSeparatesDestinationPorts(t *testing.T) {
	monitor := NewTrafficMonitor()
	for _, destinationPort := range []uint16{443, 22} {
		monitor.ObservePacket(flowPacket(net.ParseIP("10.90.0.2"), net.ParseIP("10.90.0.3"), 6, 52000, destinationPort, 40))
	}
	flows := monitor.Snapshot().Flows
	if len(flows) != 2 || flows[0].DestinationPort == nil || *flows[0].DestinationPort != 22 || flows[1].DestinationPort == nil || *flows[1].DestinationPort != 443 {
		t.Fatalf("destination ports were not separated in stable order: %+v", flows)
	}
}

func TestTrafficMonitorBoundsAndExpiresUniqueFlows(t *testing.T) {
	current := time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC)
	monitor := NewTrafficMonitor()
	monitor.now = func() time.Time { return current }
	for port := 0; port <= maxTrackedFlows; port++ {
		monitor.ObservePacket(flowPacket(net.ParseIP("10.90.0.2"), net.ParseIP("10.90.0.3"), 6, uint16(port), 443, 40))
	}
	if flows := monitor.Snapshot().Flows; len(flows) != maxTrackedFlows {
		t.Fatalf("tracked %d flows, want bounded %d", len(flows), maxTrackedFlows)
	}
	current = current.Add(trafficWindowSeconds * time.Second)
	monitor.ObservePacket(flowPacket(net.ParseIP("10.90.0.2"), net.ParseIP("10.90.0.3"), 6, 65000, 443, 40))
	if flows := monitor.Snapshot().Flows; len(flows) != 1 || flows[0].SourcePort == nil || *flows[0].SourcePort != 65000 {
		t.Fatalf("expired flow capacity was not released: %+v", flows)
	}
}

func TestTrafficMonitorFlushesCompletedHistoryOnceAndRetriesFailures(t *testing.T) {
	current := time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC)
	monitor := NewTrafficMonitor()
	monitor.now = func() time.Time { return current }
	monitor.ObservePacket(flowPacket(net.ParseIP("10.90.0.2"), net.ParseIP("10.90.0.3"), 6, 52000, 443, 40))
	var calls int
	var saved []TrafficHistoryRecord
	monitor.SetHistorySink(func(records []TrafficHistoryRecord) error {
		calls++
		if calls == 1 {
			return errors.New("temporary storage error")
		}
		saved = append(saved, records...)
		return nil
	})
	current = current.Add(time.Second)
	if err := monitor.FlushHistory(); err == nil {
		t.Fatal("history sink failure was ignored")
	}
	if err := monitor.FlushHistory(); err != nil {
		t.Fatal(err)
	}
	if err := monitor.FlushHistory(); err != nil {
		t.Fatal(err)
	}
	if calls != 2 || len(saved) != 1 {
		t.Fatalf("history was duplicated or not retried: calls=%d records=%+v", calls, saved)
	}
	record := saved[0]
	if !record.RecordedAt.Equal(current.Add(-time.Second)) || record.Protocol != "tcp" || record.SourcePort == nil || *record.SourcePort != 52000 || record.DestinationPort == nil || *record.DestinationPort != 443 || record.Bytes != 40 || record.Packets != 1 {
		t.Fatalf("unexpected history record: %+v", record)
	}
}

func TestTrafficMonitorFlushHistoryNowIncludesPartialSecond(t *testing.T) {
	monitor := NewTrafficMonitor()
	monitor.Observe(net.ParseIP("10.90.0.2"), net.ParseIP("10.90.0.3"), 80)
	var saved []TrafficHistoryRecord
	monitor.SetHistorySink(func(records []TrafficHistoryRecord) error { saved = append(saved, records...); return nil })
	if err := monitor.FlushHistoryNow(); err != nil || len(saved) != 1 || saved[0].Bytes != 80 {
		t.Fatalf("partial history was not flushed: %+v %v", saved, err)
	}
}

func TestTrafficMonitorEmptySnapshotUsesJSONArrays(t *testing.T) {
	payload, err := json.Marshal(NewTrafficMonitor().Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(payload, []byte(`"flows":[]`)) || bytes.Contains(payload, []byte(`"series":null`)) {
		t.Fatalf("empty snapshot must use arrays: %s", payload)
	}
}
