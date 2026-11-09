package sim

import (
	"context"
	"testing"
	"time"
)

func TestFaultsChargesAreConsumedOnce(t *testing.T) {
	f := NewFaults()
	if f.Take("FC3") {
		t.Fatal("nothing armed, Take must be false")
	}
	f.Arm("FC3", 2)
	if !f.Armed("FC3") {
		t.Fatal("armed fault must report Armed")
	}
	if !f.Take("FC3") || !f.Take("FC3") {
		t.Fatal("two charges armed, two Takes must fire")
	}
	if f.Take("FC3") || f.Armed("FC3") {
		t.Fatal("charges used up")
	}
}

func TestBrokenMachineStaysBrokenUntilRepaired(t *testing.T) {
	f := NewFaults()
	f.Break("thm-bake-zone-2")
	if !f.Broken("thm-bake-zone-2") {
		t.Fatal("machine must be broken")
	}
	if f.Repair("thm-cool-cooling") {
		t.Fatal("repairing a machine that was not broken must report false")
	}
	if !f.Repair("thm-bake-zone-2") || f.Broken("thm-bake-zone-2") {
		t.Fatal("repair must clear the machine")
	}
}

func TestGateBlocksWhileClosed(t *testing.T) {
	g := NewGate()
	if !g.Open() {
		t.Fatal("a new gate is open")
	}
	g.Set(false)
	if g.Open() {
		t.Fatal("gate must be closed")
	}
	done := make(chan bool)
	go func() { done <- g.Wait(context.Background()) }()
	select {
	case <-done:
		t.Fatal("Wait must block while the gate is closed")
	case <-time.After(50 * time.Millisecond):
	}
	g.Set(true)
	select {
	case ok := <-done:
		if !ok {
			t.Fatal("Wait must return true once the gate opens")
		}
	case <-time.After(time.Second):
		t.Fatal("Wait did not return after the gate opened")
	}
}

func TestGateWaitEndsWithContext(t *testing.T) {
	g := NewGate()
	g.Set(false)
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(30 * time.Millisecond); cancel() }()
	if g.Wait(ctx) {
		t.Fatal("Wait must return false when the context ends first")
	}
}

func TestSetOnAnOpenGateTwiceIsHarmless(t *testing.T) {
	g := NewGate()
	g.Set(true)
	g.Set(true)
	g.Set(false)
	g.Set(false)
	g.Set(true)
	if !g.Open() {
		t.Fatal("gate must be open")
	}
}

func TestScaleAndUnscaleAreInverse(t *testing.T) {
	d := 40 * time.Second
	if got := Unscale(Scale(d)); got < d-time.Millisecond || got > d+time.Millisecond {
		t.Fatalf("Unscale(Scale(%v)) = %v", d, got)
	}
}

func TestSleepReturnsFalseWhenContextEnds(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if Sleep(ctx, time.Hour) {
		t.Fatal("Sleep must stop at once when the context is done")
	}
}
