package mon

import (
	"context"
	"testing"

	"cakefactory/internal/model"
	"cakefactory/internal/node"
	"cakefactory/internal/sig"
	"cakefactory/internal/sparkplug"
	"cakefactory/internal/svc"
)

func TestTicketsGoToTheStationOfTheMachine(t *testing.T) {
	cases := map[string]string{
		"thm-bake-zone-2":     model.THMBake,
		"fin-wash-washer":     model.FINWash,
		"bat-mix-mixer":       model.BATMix,
		"bt-section-2":        model.BT,
		"thm-cool-controller": model.THMCool,
	}
	for machine, want := range cases {
		if got := nodeOf(machine, "fallback"); got != want {
			t.Errorf("nodeOf(%q) = %q, want %q", machine, got, want)
		}
	}
	if got := nodeOf("coffee-machine", model.PLMWh); got != model.PLMWh {
		t.Errorf("an unknown machine falls back to the node that reported it, got %q", got)
	}
}

func TestOnlyBirthAndHeartbeatCountAsAProofOfLife(t *testing.T) {
	topic := func(typ string) sparkplug.ParsedTopic {
		p, ok := sparkplug.ParseTopic(sparkplug.Topic(model.Group, typ, model.THMBake, ""))
		if !ok {
			t.Fatal("topic does not parse")
		}
		return p
	}
	hb := sparkplug.Payload{Metrics: sig.New(sig.Heartbeat, "uptime", int64(5)).Metrics()}
	ev := sparkplug.Payload{Metrics: sig.New(sig.Events, "code", "FC12").Metrics()}
	if !isHealth(topic(sparkplug.NBirth), sparkplug.Payload{}) {
		t.Error("a birth certificate proves the node is alive")
	}
	if !isHealth(topic(sparkplug.NData), hb) {
		t.Error("a heartbeat proves the node is alive")
	}
	if isHealth(topic(sparkplug.NData), ev) {
		t.Error("an event is not a heartbeat: a frozen node that still sends events must be found (FC28)")
	}
	if isHealth(topic(sparkplug.DData), hb) {
		t.Error("only node messages count")
	}
}

func offlineMonitor(t *testing.T) *Monitor {
	t.Helper()
	t.Setenv("OUTBOX_DIR", t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	b := &svc.Base{ID: model.MON, Ctx: ctx, N: node.New(node.Config{ID: model.MON, Host: true})}
	m := &Monitor{Base: b, nodes: map[string]*NodeState{}, tickets: map[string]model.Ticket{}, silent: map[string]bool{}, haltNodes: map[string]bool{}}
	for _, id := range model.Nodes {
		if id != model.MON {
			m.nodes[id] = &NodeState{ID: id, Status: "unknown", Devices: map[string]DevState{}, Gauges: map[string]int64{}}
		}
	}
	return m
}

func birth(bd int64) sparkplug.Payload {
	return sparkplug.Payload{Metrics: []sparkplug.Metric{sparkplug.NewMetric("bdSeq", bd)}}
}

func TestFirstBirthAfterADeathCertificateReleasesTheSections(t *testing.T) {
	// The monitor restarts together with the stations: it sees the death certificate of the old
	// container before the first birth of the new one. The sections it halted must run again.
	m := offlineMonitor(t)
	m.haltNodes[model.THMDep] = true
	m.onBirth(m.nodes[model.THMDep], birth(1))
	if m.haltNodes[model.THMDep] {
		t.Fatal("a station that is back must not keep its band sections halted")
	}
	if m.nodes[model.THMDep].Status != "online" {
		t.Fatal("the station is online after its birth certificate")
	}
}

func TestRestartReleasesTheSectionsToo(t *testing.T) {
	m := offlineMonitor(t)
	m.onBirth(m.nodes[model.FINDpn], birth(1))
	m.haltNodes[model.FINDpn] = true
	m.nodes[model.FINDpn].Status = "dead"
	m.onBirth(m.nodes[model.FINDpn], birth(2))
	if m.haltNodes[model.FINDpn] || m.nodes[model.FINDpn].Restarts != 1 {
		t.Fatalf("restart: halted %v, restarts %d", m.haltNodes[model.FINDpn], m.nodes[model.FINDpn].Restarts)
	}
}

func TestSilentStationStaysHaltedUntilItIsRepaired(t *testing.T) {
	m := offlineMonitor(t)
	m.haltNodes[model.THMBake] = true
	m.silent[model.THMBake] = true // FC28: a heartbeat-less station is handled through its maintenance ticket
	m.onBirth(m.nodes[model.THMBake], birth(1))
	if !m.haltNodes[model.THMBake] {
		t.Fatal("a station marked faulty (FC28) is released by the repair, not by a birth certificate")
	}
}

func TestStationThatWasNeverHaltedIsLeftAlone(t *testing.T) {
	m := offlineMonitor(t)
	m.onBirth(m.nodes[model.BATMix], birth(1))
	if len(m.haltNodes) != 0 {
		t.Fatalf("nothing to release: %v", m.haltNodes)
	}
}
