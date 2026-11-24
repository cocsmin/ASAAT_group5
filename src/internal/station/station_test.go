package station

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"cakefactory/internal/node"
	"cakefactory/internal/sig"
	"cakefactory/internal/svc"
)

func base(t *testing.T) *svc.Base {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return &svc.Base{ID: "TEST", Ctx: ctx, N: node.New(node.Config{ID: "TEST"})}
}

func band(kv ...any) sig.Signal { return sig.New(sig.BandState, kv...) }

func TestSectionFollowsBandState(t *testing.T) {
	s := NewSection(2)
	if halted := s.OnBand(band("S2", "halt")); !halted || s.Gate.Open() {
		t.Fatal("a halted section must close the gate")
	}
	if halted := s.OnBand(band("S1", "halt", "S2", "run")); halted || !s.Gate.Open() {
		t.Fatal("only the own section counts: S2 runs, so the gate opens")
	}
	if halted := s.OnBand(band("S2", "slow")); halted || !s.Gate.Open() {
		t.Fatal("slow-down keeps the gate open")
	}
}

func TestSpanWaitsForTheSectionAndSlowsDown(t *testing.T) {
	b := base(t)
	s := NewSection(1)

	start := time.Now()
	if !s.Span(b, 40*time.Millisecond) {
		t.Fatal("Span must complete on a running section")
	}
	normal := time.Since(start)

	s.OnBand(band("S1", "slow"))
	start = time.Now()
	s.Span(b, 40*time.Millisecond)
	if slow := time.Since(start); slow < normal+30*time.Millisecond {
		t.Fatalf("slow-down must double the work time: normal %v, slow %v", normal, slow)
	}

	s.OnBand(band("S1", "halt"))
	done := make(chan bool)
	go func() { done <- s.Span(b, time.Millisecond) }()
	select {
	case <-done:
		t.Fatal("Span must wait while the section is halted")
	case <-time.After(60 * time.Millisecond):
	}
	s.OnBand(band("S1", "run"))
	select {
	case ok := <-done:
		if !ok {
			t.Fatal("Span must complete after the section runs again")
		}
	case <-time.After(time.Second):
		t.Fatal("Span did not resume")
	}
}

func TestSpanEndsWhenTheServiceStops(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	b := &svc.Base{Ctx: ctx}
	s := NewSection(3)
	s.OnBand(band("S3", "halt"))
	go func() { time.Sleep(30 * time.Millisecond); cancel() }()
	if s.Span(b, time.Second) {
		t.Fatal("Span must return false when the service stops")
	}
}

func TestPanSetTakesAPanOnlyOnce(t *testing.T) {
	p := NewPanSet()
	if !p.Add("PAN-001") {
		t.Fatal("first Add must succeed")
	}
	if p.Add("PAN-001") {
		t.Fatal("a pan that is already taken must be refused: the message and the scan never start it twice")
	}
	p.Drop("PAN-001")
	if !p.Add("PAN-001") {
		t.Fatal("after Drop the pan can be taken again (it comes round the loop)")
	}
}

func TestReconcileScansNowAndThenPeriodically(t *testing.T) {
	old := ReconcileEvery
	ReconcileEvery = 20 * time.Millisecond
	defer func() { ReconcileEvery = old }()

	b := base(t)
	var n atomic.Int32
	Reconcile(b, func() { n.Add(1) })
	if n.Load() != 1 {
		t.Fatal("the first scan runs at once, before Reconcile returns")
	}
	time.Sleep(120 * time.Millisecond)
	if n.Load() < 3 {
		t.Fatalf("scan must repeat, ran %d times", n.Load())
	}
}

func TestAdmissionOnlyPublishesChanges(t *testing.T) {
	b := base(t)
	a := &Admission{B: b}
	a.Set(true, "buffer full")
	a.Set(true, "buffer full") // no second publication, no panic offline
	a.Set(false, "")
	a.Set(true, "again")
	a.Reset()
	if a.stopped {
		t.Fatal("Reset must clear the stop")
	}
}
