package mon

import (
	"fmt"
	"testing"
	"time"

	"cakefactory/internal/model"
	"cakefactory/internal/sig"
	"cakefactory/internal/sparkplug"
	"cakefactory/internal/timing"
)

// Scalability argument for the numbers of the design (docs/scalability.md): every node sends one
// heartbeat every T_hb = 5 s, so the load on the monitor grows with the number of nodes divided by
// 5 s. These tests measure what one heartbeat costs (bytes on the wire, time to decode and take
// in) and check that the 16 containers of the factory, and a plant 100 times bigger, stay far
// below what one monitor process and one broker connection can carry.

// heartbeat is a heartbeat as a station sends it: uptime, two device states, three gauges.
func heartbeat() sparkplug.Payload {
	s := sig.New(sig.Heartbeat, "uptime", int64(1234),
		"dev.Depositor", "ok", "dev.CheckWeigher", "ok",
		"g.queue", int64(2), "g.deposited", int64(310), "g.batches", int64(12))
	return sparkplug.Payload{Metrics: s.Metrics()}
}

// load takes in n heartbeats, spread over nodes node states, and returns the cost of one.
func load(t *testing.T, nodes, n int) (perBeat time.Duration, bytes int) {
	t.Helper()
	m := offlineMonitor(t)
	states := make([]*NodeState, nodes)
	for i := range states {
		id := fmt.Sprintf("NODE-%04d", i)
		states[i] = &NodeState{ID: id, Status: "online", Devices: map[string]DevState{}, Gauges: map[string]int64{}}
		m.nodes[id] = states[i]
	}
	wire := heartbeat().Marshal()
	bytes = len(wire)
	start := time.Now()
	for i := 0; i < n; i++ {
		p, err := sparkplug.Unmarshal(wire) // what the broker delivers
		if err != nil {
			t.Fatal(err)
		}
		m.onHeartbeat(states[i%nodes], p)
	}
	return time.Since(start) / time.Duration(n), bytes
}

func TestMonitorCarriesTheHeartbeatsOfAMuchBiggerPlant(t *testing.T) {
	const beats = 20000
	var worst time.Duration
	t.Logf("%8s %12s %14s %14s %12s", "nodes", "msgs/s", "bytes/s", "per heartbeat", "CPU of 1 core")
	for _, nodes := range []int{len(model.Nodes), 160, 1600} {
		per, size := load(t, nodes, beats)
		rate := float64(nodes) / timing.Heartbeat.Seconds() // heartbeats per second
		cpu := rate * per.Seconds() * 100
		t.Logf("%8d %12.1f %14.0f %14s %11.3f %%", nodes, rate, rate*float64(size), per.Round(time.Microsecond/10), cpu)
		if per > worst {
			worst = per
		}
		if nodes == 1600 {
			if cpu > 10 {
				t.Errorf("1600 nodes cost %.1f %% of one core just in heartbeats", cpu)
			}
			if rate*float64(size) > 100*1024 {
				t.Errorf("1600 nodes send %.0f bytes/s of heartbeats", rate*float64(size))
			}
		}
	}
	if worst > time.Millisecond {
		t.Errorf("one heartbeat takes %s to take in, more than 1 ms", worst)
	}
}

// The detection time must not depend on the number of nodes: the watchdog compares every node with
// the limit once a second (a linear scan), so it stays far below the 1 s tick even for a big plant.
func TestWatchdogScanStaysBelowTheTickForABigPlant(t *testing.T) {
	m := offlineMonitor(t)
	now := time.Now()
	for i := 0; i < 5000; i++ {
		id := fmt.Sprintf("NODE-%04d", i)
		m.nodes[id] = &NodeState{ID: id, Status: "online", LastSeen: now, Devices: map[string]DevState{}, Gauges: map[string]int64{}}
	}
	limit := timing.WatchdogMisses * timing.Heartbeat
	start := time.Now()
	silent := 0
	m.mu.Lock()
	for _, n := range m.nodes { // the same test as in watchdog()
		if n.Status == "online" && !n.LastSeen.IsZero() && time.Since(n.LastSeen) > limit {
			silent++
		}
	}
	m.mu.Unlock()
	took := time.Since(start)
	t.Logf("scan of %d nodes: %s (the watchdog ticks once a second)", len(m.nodes), took)
	if silent != 0 {
		t.Fatalf("no node is silent, got %d", silent)
	}
	if took > 100*time.Millisecond {
		t.Errorf("a scan of 5000 nodes takes %s", took)
	}
}
