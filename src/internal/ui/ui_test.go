package ui

import (
	"strings"
	"testing"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"

	"cakefactory/internal/model"
	"cakefactory/internal/sig"
	"cakefactory/internal/sparkplug"
)

// fakeMsg is an MQTT message the observer can be fed with.
type fakeMsg struct {
	topic   string
	payload []byte
}

var _ mqtt.Message = fakeMsg{}

func (fakeMsg) Duplicate() bool   { return false }
func (fakeMsg) Qos() byte         { return 0 }
func (fakeMsg) Retained() bool    { return false }
func (m fakeMsg) Topic() string   { return m.topic }
func (fakeMsg) MessageID() uint16 { return 0 }
func (m fakeMsg) Payload() []byte { return m.payload }
func (fakeMsg) Ack()              {}

func send(o *observer, typ, edge, device string, metrics ...sparkplug.Metric) {
	p := sparkplug.Payload{Metrics: metrics}
	o.onMessage(nil, fakeMsg{topic: sparkplug.Topic(model.Group, typ, edge, device), payload: p.Marshal()})
}

func heartbeat(kv ...any) []sparkplug.Metric {
	return sig.New(sig.Heartbeat, kv...).Metrics()
}

func newObs() *observer { return newObserver(5*time.Second, func() {}) }

func status(o *observer, id string) NodeState {
	_, stations, _, _ := o.snapshot()
	for _, n := range stations {
		if n.ID == id {
			return n
		}
	}
	return NodeState{}
}

func TestBirthAndHeartbeatMakeAStationOnline(t *testing.T) {
	o := newObs()
	if got := status(o, model.THMBake).Status; got != "unknown" {
		t.Fatalf("before any message the station is unknown, got %s", got)
	}
	send(o, sparkplug.NBirth, model.THMBake, "", sparkplug.NewMetric("bdSeq", int64(1)))
	send(o, sparkplug.NData, model.THMBake, "", heartbeat("uptime", int64(42), "dev.Zone1", "OK", "g.tunnel", int64(6))...)
	n := status(o, model.THMBake)
	if n.Status != "online" || n.Uptime != 42 || n.Gauges["tunnel"] != 6 || n.Devices["Zone1"].State != "OK" {
		t.Fatalf("station state: %+v", n)
	}
}

func TestDeathCertificateMarksTheStationDeadAndItsDevicesOffline(t *testing.T) {
	o := newObs()
	send(o, sparkplug.NBirth, model.FINWash, "", sparkplug.NewMetric("bdSeq", int64(1)))
	send(o, sparkplug.NData, model.FINWash, "", heartbeat("dev.PanPool", "OK")...)
	send(o, sparkplug.NDeath, model.FINWash, "")
	n := status(o, model.FINWash)
	if n.Status != "dead" || n.Devices["PanPool"].State != model.StateOffline {
		t.Fatalf("after NDEATH: %+v", n)
	}
}

func TestRestartIsCountedFromANewBirth(t *testing.T) {
	o := newObs()
	send(o, sparkplug.NBirth, model.BATMix, "", sparkplug.NewMetric("bdSeq", int64(1)))
	send(o, sparkplug.NDeath, model.BATMix, "")
	send(o, sparkplug.NBirth, model.BATMix, "", sparkplug.NewMetric("bdSeq", int64(2)))
	n := status(o, model.BATMix)
	if n.Restarts != 1 || n.Status != "online" {
		t.Fatalf("restart: %+v", n)
	}
}

func TestSilentStationIsDetectedFromMissingHeartbeats(t *testing.T) {
	o := newObs()
	send(o, sparkplug.NBirth, model.THMExit, "", sparkplug.NewMetric("bdSeq", int64(1)))
	o.mu.Lock()
	o.nodes[model.THMExit].LastSeen = time.Now().Add(-20 * time.Second) // more than 3 x 5 s
	o.mu.Unlock()
	done := make(chan struct{})
	go o.watch(done)
	defer close(done)
	deadline := time.After(3 * time.Second)
	for status(o, model.THMExit).Status != "silent" {
		select {
		case <-deadline:
			t.Fatal("a station without heartbeats for 3 x T_hb must become silent")
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func TestBandStateFromBTSetsTheSections(t *testing.T) {
	o := newObs()
	send(o, sparkplug.NBirth, model.BT, "", sparkplug.NewMetric("bdSeq", int64(1)))
	send(o, sparkplug.NData, model.BT, "", sig.New(sig.BandState, "S1", "halt", "D1", "fault", "S2", "run", "D2", "run").Metrics()...)
	_, _, secs, _ := o.snapshot()
	if secs[0].State != "fault" || secs[1].State != "run" {
		t.Fatalf("D1 carries the detail incl. fault: %+v", secs)
	}
}

func TestSectionsAreUnknownWhileBandTransportIsDown(t *testing.T) {
	o := newObs() // BT never came online
	_, _, secs, _ := o.snapshot()
	for _, s := range secs {
		if s.State != "off" {
			t.Fatalf("section %d must be unknown (off) without BT, got %s", s.ID, s.State)
		}
	}
}

func TestMonitorStatusComesFromTheHostStateMessage(t *testing.T) {
	o := newObs()
	if got := status(o, model.MON).Status; got != "unknown" {
		t.Fatalf("before the first STATE message MON is unknown, got %s", got)
	}
	o.onMessage(nil, fakeMsg{topic: "spBv1.0/STATE/MON", payload: []byte(`{"online":true}`)})
	if got := status(o, model.MON).Status; got != "online" {
		t.Fatalf("got %s", got)
	}
	o.onMessage(nil, fakeMsg{topic: "spBv1.0/STATE/MON", payload: []byte(`{"online":false}`)})
	n := status(o, model.MON)
	if n.Status != "dead" || !strings.Contains(n.Note, "offline") {
		t.Fatalf("MON offline: %+v", n)
	}
}

func TestSystemsAreMergedFromTheirStations(t *testing.T) {
	o := newObs()
	for _, id := range []string{model.THMDep, model.THMBake, model.THMExit, model.THMCool} {
		send(o, sparkplug.NBirth, id, "", sparkplug.NewMetric("bdSeq", int64(1)))
	}
	send(o, sparkplug.NData, model.THMExit, "", heartbeat("g.discarded", int64(2), "dev.HeatCamera", "OK")...)
	send(o, sparkplug.NData, model.THMCool, "", heartbeat("g.discarded", int64(3), "g.buffer", int64(4))...)
	send(o, sparkplug.NDeath, model.THMBake, "")

	systems, stations, _, _ := o.snapshot()
	var thm NodeState
	for _, s := range systems {
		if s.ID == model.THM {
			thm = s
		}
	}
	if thm.Status != "dead" || !strings.Contains(thm.Note, model.THMBake) {
		t.Fatalf("a dead station makes its system dead and says which one: %+v", thm)
	}
	if thm.Gauges["discarded"] != 5 || thm.Gauges["buffer"] != 4 {
		t.Fatalf("gauges are added up across the stations: %v", thm.Gauges)
	}
	if len(thm.Stations) != 4 || len(stations) != len(model.Nodes) {
		t.Fatalf("stations %v, all containers %d", thm.Stations, len(stations))
	}
	if len(systems) != len(model.Systems) {
		t.Fatalf("one entry per system, got %d", len(systems))
	}
}

func TestMergedStatusTakesTheWorst(t *testing.T) {
	by := map[string]NodeState{
		"A-1": {ID: "A-1", Status: "online", Uptime: 90},
		"A-2": {ID: "A-2", Status: "silent", Uptime: 30},
		"A-3": {ID: "A-3", Status: "unknown"},
	}
	m := mergeStations("A", []string{"A-1", "A-2", "A-3"}, by)
	if m.Status != "silent" {
		t.Fatalf("silent is worse than unknown and online, got %s", m.Status)
	}
}

func TestAlertsListWhatTheOverseerMustLookAt(t *testing.T) {
	now := time.Now()
	stations := []NodeState{
		{ID: model.THMBake, Status: "silent", LastSeen: now},
		{ID: model.FINWash, Status: "dead"},
		{ID: model.MON, Status: "dead"},
		{ID: model.BATMix, Status: "online"},
	}
	secs := []Section{{ID: 1, Name: "Depositing", State: "halt"}, {ID: 2, Name: "Baking tunnel", State: "run"}, {ID: 3, State: "off"}}
	tickets := []model.Ticket{
		{ID: 1, Status: "open", MachineID: "thm-bake-zone-2", Reason: "zone", Node: model.THMBake, Opened: now},
		{ID: 2, Status: "repaired", MachineID: "x"},
	}
	events := []model.LogEvent{
		{Severity: model.SevWarning, At: now, Node: model.BATDose, Code: "FC1", Reason: "not enough flour"},
		{Severity: model.SevWarning, At: now.Add(-10 * time.Minute), Node: model.BATDose, Code: "FC1", Reason: "old warning"},
	}
	out := alerts(stations, secs, tickets, events)
	var titles []string
	for _, a := range out {
		titles = append(titles, a.Level+": "+a.Title)
	}
	all := strings.Join(titles, "\n")
	for _, want := range []string{"Maintenance needed: thm-bake-zone-2", "THM-BAKE is not responding", "FIN-WASH container is down",
		"Monitor is offline", "Band section 1 (Depositing): halt", "not enough flour"} {
		if !strings.Contains(all, want) {
			t.Errorf("missing alert %q in:\n%s", want, all)
		}
	}
	for _, not := range []string{"x", "old warning", "Band section 2", "Band section 3", "BAT-MIX"} {
		if strings.Contains(all, not) && not != "x" {
			t.Errorf("unexpected alert %q in:\n%s", not, all)
		}
	}
	if out[0].Level != "error" {
		t.Error("errors come before warnings")
	}
}
