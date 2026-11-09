package sparkplug

import "testing"

func TestRoundTrip(t *testing.T) {
	in := Payload{
		Timestamp: 1700000000123,
		Seq:       7,
		Metrics: []Metric{
			NewMetric("a/str", "hello"),
			NewMetric("a/int", int64(-42)),
			NewMetric("a/float", 97.25),
			NewMetric("a/bool", true),
		},
	}
	out, err := Unmarshal(in.Marshal())
	if err != nil {
		t.Fatal(err)
	}
	if out.Timestamp != in.Timestamp || out.Seq != 7 || len(out.Metrics) != 4 {
		t.Fatalf("bad payload %+v", out)
	}
	want := []any{"hello", int64(-42), 97.25, true}
	for i, m := range out.Metrics {
		if m.Value != want[i] {
			t.Errorf("metric %s = %v (%T), want %v", m.Name, m.Value, m.Value, want[i])
		}
	}
}

func TestTruncated(t *testing.T) {
	b := Payload{Metrics: []Metric{NewMetric("x", "y")}}.Marshal()
	if _, err := Unmarshal(b[:4]); err == nil {
		t.Fatal("expected error")
	}
}

func TestTopic(t *testing.T) {
	topic := Topic("CakeFactory", NData, "THM", "")
	p, ok := ParseTopic(topic)
	if !ok || p.Edge != "THM" || p.Type != NData || p.Group != "CakeFactory" {
		t.Fatalf("bad parse %+v", p)
	}
	p, ok = ParseTopic(Topic("CakeFactory", DData, "BAT", "Doser"))
	if !ok || p.Device != "Doser" {
		t.Fatalf("bad parse %+v", p)
	}
}
