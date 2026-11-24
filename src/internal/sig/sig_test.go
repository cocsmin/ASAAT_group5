package sig

import (
	"testing"

	"cakefactory/internal/sparkplug"
)

func TestSignalRoundTrip(t *testing.T) {
	in := New(BatterReady, "batchId", "B0001", "pans", int64(6), "fillTarget", 450.0, "ok", true)
	p, err := sparkplug.Unmarshal(sparkplug.Payload{Metrics: in.Metrics()}.Marshal())
	if err != nil {
		t.Fatal(err)
	}
	got := FromMetrics(p.Metrics)
	if len(got) != 1 || got[0].Name != BatterReady {
		t.Fatalf("got %+v", got)
	}
	s := got[0]
	if s.Str("batchId") != "B0001" || s.Int("pans") != 6 || s.Float("fillTarget") != 450 || !s.Bool("ok") {
		t.Fatalf("fields wrong: %+v", s.F)
	}
}

func TestTwoSignalsInOnePayload(t *testing.T) {
	a, b := New(PanRequest, "message", "x"), New(Heartbeat, "uptime", int64(3))
	got := FromMetrics(append(a.Metrics(), b.Metrics()...))
	if len(got) != 2 {
		t.Fatalf("want 2 signals, got %d", len(got))
	}
}
