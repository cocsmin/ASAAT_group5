package node

import (
	"sync"
	"testing"

	"cakefactory/internal/model"
	"cakefactory/internal/sig"
	"cakefactory/internal/sparkplug"
)

// payload makes the bytes of an NDATA message carrying the given signals.
func payload(sigs ...sig.Signal) []byte {
	var ms []sparkplug.Metric
	for _, s := range sigs {
		ms = append(ms, s.Metrics()...)
	}
	return sparkplug.Payload{Metrics: ms}.Marshal()
}

func topic(typ, edge string) string { return sparkplug.Topic(model.Group, typ, edge, "") }

type recorder struct {
	mu   sync.Mutex
	from []string
}

func (r *recorder) add(from string, _ sig.Signal) {
	r.mu.Lock()
	r.from = append(r.from, from)
	r.mu.Unlock()
}

func (r *recorder) n() int { r.mu.Lock(); defer r.mu.Unlock(); return len(r.from) }

func TestSignalGoesOnlyToHandlersOfTheSendingNode(t *testing.T) {
	n := New(Config{ID: model.THMBake})
	fromDep, fromAny := &recorder{}, &recorder{}
	n.OnSignal(model.THMDep, sig.PanToBaking, fromDep.add)
	n.OnSignal("", sig.PanToBaking, fromAny.add)

	n.dispatch(topic(sparkplug.NData, model.THMDep), payload(sig.New(sig.PanToBaking, "panId", "PAN-001")))
	n.dispatch(topic(sparkplug.NData, model.THMCool), payload(sig.New(sig.PanToBaking, "panId", "PAN-002")))

	if fromDep.n() != 1 {
		t.Errorf("handler for THM-DEP got %d messages, want 1", fromDep.n())
	}
	if fromAny.n() != 2 {
		t.Errorf("handler for any node got %d messages, want 2", fromAny.n())
	}
}

func TestDuplicateMessageIdIsDropped(t *testing.T) {
	n := New(Config{ID: model.THMBake})
	r := &recorder{}
	n.OnSignal(model.THMDep, sig.PanToBaking, r.add)
	msg := payload(sig.New(sig.PanToBaking, "panId", "PAN-001", "msgId", "THM-DEP-1-1"))
	n.dispatch(topic(sparkplug.NData, model.THMDep), msg)
	n.dispatch(topic(sparkplug.NData, model.THMDep), msg) // QoS 1 delivers at least once
	if r.n() != 1 {
		t.Fatalf("the same message id must be handled once, handled %d times", r.n())
	}
	n.dispatch(topic(sparkplug.NData, model.THMDep), payload(sig.New(sig.PanToBaking, "panId", "PAN-002", "msgId", "THM-DEP-1-2")))
	if r.n() != 2 {
		t.Fatal("a new message id must be handled")
	}
}

func TestSameIdFromAnotherNodeIsNotADuplicate(t *testing.T) {
	n := New(Config{ID: model.FINWash})
	r := &recorder{}
	n.OnSignal("", sig.PanForWashing, r.add)
	msg := payload(sig.New(sig.PanForWashing, "panId", "PAN-001", "msgId", "7"))
	n.dispatch(topic(sparkplug.NData, model.THMExit), msg)
	n.dispatch(topic(sparkplug.NData, model.FINDpn), msg)
	if r.n() != 2 {
		t.Fatalf("message ids are per sending node, handled %d of 2", r.n())
	}
}

func TestNodeIgnoresItsOwnMessages(t *testing.T) {
	n := New(Config{ID: model.THMBake})
	r := &recorder{}
	n.OnSignal("", sig.PanToBaking, r.add)
	n.dispatch(topic(sparkplug.NData, model.THMBake), payload(sig.New(sig.PanToBaking, "panId", "PAN-001")))
	if r.n() != 0 {
		t.Fatal("a node must not react to what it sent itself")
	}
}

func TestCommandReachesOnlyTheAddressedNode(t *testing.T) {
	n := New(Config{ID: model.THMBake})
	var got []string
	n.OnCommand(sig.FaultInjection, func(s sig.Signal) { got = append(got, s.Str("code")) })
	n.dispatch(topic(sparkplug.NCmd, model.THMBake), payload(sig.New(sig.FaultInjection, "code", "FC11", "count", int64(1))))
	n.dispatch(topic(sparkplug.NCmd, model.THMExit), payload(sig.New(sig.FaultInjection, "code", "FC13", "count", int64(1))))
	if len(got) != 1 || got[0] != "FC11" {
		t.Fatalf("only the command for THM-BAKE must arrive, got %v", got)
	}
}

func TestHostStateTogglesTheMonitorRule(t *testing.T) {
	n := New(Config{ID: model.THMDep})
	var states []bool
	n.OnHost(func(on bool) { states = append(states, on) })
	n.dispatch("spBv1.0/STATE/MON", []byte(`{"online":true}`))
	n.dispatch("spBv1.0/STATE/MON", []byte(`{"online":true}`)) // repeated: no change
	if !n.HostOnline() {
		t.Fatal("host must be online")
	}
	n.dispatch("spBv1.0/STATE/MON", []byte(`{"online":false}`))
	if n.HostOnline() {
		t.Fatal("host must be offline")
	}
	if len(states) != 2 || !states[0] || states[1] {
		t.Fatalf("handlers must hear each change once, got %v", states)
	}
}

func TestGaugesAddUpAndRestore(t *testing.T) {
	n := New(Config{ID: model.THMDep})
	n.Gauge("queue", 3)
	n.AddGauge("deposited", 1)
	n.AddGauge("deposited", 2)
	if n.GaugeValue("deposited") != 3 || n.GaugeValue("queue") != 3 {
		t.Fatalf("gauges: %v", n.Gauges())
	}
	m := New(Config{ID: model.THMDep})
	m.RestoreGauges(n.Gauges())
	if m.GaugeValue("deposited") != 3 {
		t.Fatal("a restarted node must get its counters back from the saved state")
	}
}
