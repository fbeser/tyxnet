package routing

import (
	"math"
	"net"
	"testing"
	"time"
)

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
