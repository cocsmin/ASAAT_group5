package thm

import (
	"context"
	"testing"
	"time"

	"cakefactory/internal/model"
	"cakefactory/internal/node"
	"cakefactory/internal/sim"
	"cakefactory/internal/station"
	"cakefactory/internal/store"
	"cakefactory/internal/svc"
)

// testBase is a station base that is not connected to a broker: sends are dropped,
// the pan documents live in a temporary directory.
func testBase(t *testing.T, id string) (*svc.Base, store.Pans) {
	t.Helper()
	t.Setenv("OUTBOX_DIR", t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	pans, err := store.NewFilePans(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// cleanups run last to first: stop the station, give its goroutines a moment, then remove the files
	t.Cleanup(func() { time.Sleep(300 * time.Millisecond) })
	t.Cleanup(cancel)
	return &svc.Base{ID: id, Ctx: ctx, N: node.New(node.Config{ID: id}), F: sim.NewFaults(), Pans: pans}, pans
}

func put(t *testing.T, pans store.Pans, id, stationName, status string, age time.Duration, fill float64) {
	t.Helper()
	err := pans.Save(context.Background(), model.Pan{ID: id, BatchID: "B0001", Station: stationName, Status: status,
		FillWeight: fill, CakeTemp: 97, Updated: time.Now().Add(-age)})
	if err != nil {
		t.Fatal(err)
	}
}

func taken(set *station.PanSet, id string) bool { return !set.Add(id) }

func status(t *testing.T, pans store.Pans, id string) string {
	t.Helper()
	p, _, _ := pans.Get(context.Background(), id)
	return p.Status
}

func TestBakeAdoptsPansDepositingHandedOverAndSkipsFreshOnes(t *testing.T) {
	b, pans := testBase(t, model.THMBake)
	s := &Bake{Base: b, sec: station.NewSection(2), took: station.NewPanSet(), adm: &station.Admission{B: b}}
	ok := store.SeedRecipe.FillTarget
	put(t, pans, "PAN-001", "THM-Depositing", "filled", 10*time.Second, ok) // handed over, message missed
	put(t, pans, "PAN-002", "THM-Depositing", "filled", 0, ok)              // just handed over: the message wins
	put(t, pans, "PAN-003", "THM-Baking", "baking", 0, ok)                  // was inside the tunnel at a restart
	put(t, pans, "PAN-004", "FIN-PanPool", "clean", time.Hour, 0)           // not ours
	s.adopt()
	if !taken(s.took, "PAN-001") || !taken(s.took, "PAN-003") {
		t.Fatal("an old handed-over pan and a pan that was baking must be taken up")
	}
	if taken(s.took, "PAN-002") {
		t.Fatal("a pan handed over a moment ago waits for its message")
	}
	if taken(s.took, "PAN-004") {
		t.Fatal("a clean pan in the pool is none of the tunnel's business")
	}
}

func TestBakeDivertsAPanThatWasFilledOutOfTolerance(t *testing.T) {
	b, pans := testBase(t, model.THMBake)
	s := &Bake{Base: b, sec: station.NewSection(2), took: station.NewPanSet(), adm: &station.Admission{B: b}}
	put(t, pans, "PAN-001", "THM-Depositing", "filled", 10*time.Second, store.SeedRecipe.FillTarget+3*store.SeedRecipe.FillTol)
	s.adopt()
	if got := status(t, pans, "PAN-001"); got != "discarded" {
		t.Fatalf("a pan with a bad fill weight must be diverted at adoption, status %s", got)
	}
	if taken(s.took, "PAN-001") {
		t.Fatal("a diverted pan is not in the tunnel")
	}
}

func TestAdoptedPanStartsBaking(t *testing.T) {
	b, pans := testBase(t, model.THMBake)
	s := &Bake{Base: b, sec: station.NewSection(2), took: station.NewPanSet(), adm: &station.Admission{B: b}}
	put(t, pans, "PAN-001", "THM-Depositing", "filled", 10*time.Second, store.SeedRecipe.FillTarget)
	s.adopt()
	deadline := time.After(2 * time.Second)
	for status(t, pans, "PAN-001") != "baking" {
		select {
		case <-deadline:
			t.Fatal("the adopted pan must start baking")
		case <-time.After(20 * time.Millisecond):
		}
	}
}

func TestExitAdoptsPansLeavingTheTunnel(t *testing.T) {
	b, pans := testBase(t, model.THMExit)
	s := &Exit{Base: b, sec: station.NewSection(3), took: station.NewPanSet()}
	put(t, pans, "PAN-001", "THM-Baking", "tunnel-exit", 10*time.Second, 450)
	put(t, pans, "PAN-002", "THM-Baking", "tunnel-exit", 0, 450)
	put(t, pans, "PAN-003", "THM-Baking", "baking", 10*time.Second, 450)
	s.adopt()
	if !taken(s.took, "PAN-001") {
		t.Fatal("a pan that left the tunnel must be taken up")
	}
	if taken(s.took, "PAN-002") || taken(s.took, "PAN-003") {
		t.Fatal("a fresh handover and a pan still inside the tunnel are not for the exit check")
	}
}

func TestCoolAdoptsCheckedCakesAndCakesInsideCooling(t *testing.T) {
	b, pans := testBase(t, model.THMCool)
	s := &Cool{Base: b, sec: station.NewSection(3), took: station.NewPanSet(), adm: &station.Admission{B: b}}
	put(t, pans, "PAN-001", "THM-TunnelExit", "baked", 10*time.Second, 450)
	put(t, pans, "PAN-002", "THM-Cooling", "cooling", 0, 450)
	put(t, pans, "PAN-003", "THM-TunnelExit", "baked", 0, 450)
	put(t, pans, "PAN-004", "THM", "discarded", 10*time.Second, 450)
	s.adopt()
	if !taken(s.took, "PAN-001") || !taken(s.took, "PAN-002") {
		t.Fatal("a cake that passed the exit check and a cake inside the cooling must be taken up")
	}
	if taken(s.took, "PAN-003") || taken(s.took, "PAN-004") {
		t.Fatal("a fresh handover waits for its message, a diverted pan is gone")
	}
}

func TestCoolRestoresTheBufferAndKeepsTheWaitingTime(t *testing.T) {
	b, pans := testBase(t, model.THMCool)
	s := &Cool{Base: b, sec: station.NewSection(3), took: station.NewPanSet(), adm: &station.Admission{B: b}}
	put(t, pans, "PAN-001", "THM-CoolingBuffer", "buffered", 20*time.Second, 450)
	s.restoreBuffered()
	if len(s.buf) != 1 {
		t.Fatalf("buffer has %d cakes, want 1", len(s.buf))
	}
	waited := time.Since(s.buf[0].since)
	if waited < 19*time.Second || waited > 22*time.Second {
		t.Fatalf("the waiting time continues from the time the cake was put in: %v", waited)
	}
}

func TestBufferStopsDepositingWhenFullAndDivertsTheNextCake(t *testing.T) {
	b, pans := testBase(t, model.THMCool)
	s := &Cool{Base: b, sec: station.NewSection(3), took: station.NewPanSet(), adm: &station.Admission{B: b}}
	for i := 0; i < bufferCapacity; i++ {
		s.buffer(pan{id: "PAN-" + string(rune('A'+i%26)) + string(rune('a'+i/26)), batch: "B1"}, 60, "cooling out of service")
	}
	if len(s.buf) != bufferCapacity {
		t.Fatalf("buffer holds %d, capacity %d", len(s.buf), bufferCapacity)
	}
	s.buffer(pan{id: "PAN-OVER", batch: "B1"}, 60, "cooling out of service")
	if len(s.buf) != bufferCapacity {
		t.Fatal("a full buffer takes no more cakes")
	}
	if got := status(t, pans, "PAN-OVER"); got != "discarded" {
		t.Fatalf("the cake that does not fit is diverted to refuse, status %q", got)
	}
}

func TestDivertSendsThePanToWashingAndCountsTheDiscard(t *testing.T) {
	b, pans := testBase(t, model.THMExit)
	divert(b, pan{id: "PAN-001", batch: "B1"}, "FC12", "overcooked")
	p, _, _ := pans.Get(context.Background(), "PAN-001")
	if p.Status != "discarded" || p.Station != "THM" {
		t.Fatalf("diverted pan: %+v", p)
	}
	if b.N.GaugeValue("discarded") != 1 {
		t.Fatal("a diverted pan is counted")
	}
}

func TestMachineIdsOfTheStationsMatchTheirOwnNodes(t *testing.T) {
	for machine, node := range map[string]string{depositorMachine: model.THMDep, zoneMachine: model.THMBake,
		cameraMachine: model.THMExit, coolingMachine: model.THMCool} {
		if got := model.NodeOfMachine(machine, ""); got != node {
			t.Errorf("%s belongs to %s, the ticket would go to %q", machine, node, got)
		}
	}
}

func newDep(b *svc.Base) *Dep {
	return &Dep{Base: b, sec: station.NewSection(1), batters: make(chan batch, 16), clean: make(chan string, 16),
		reasons: map[string]string{}, inClean: map[string]bool{}, seenBatch: map[string]bool{}, pend: map[string]*queued{}}
}

func waitSaved(t *testing.T, pans store.Pans, want int64) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		if st, _ := pans.LoadState(context.Background(), queueKey); st["B0007.left"] == want {
			return
		}
		select {
		case <-deadline:
			st, _ := pans.LoadState(context.Background(), queueKey)
			t.Fatalf("saved queue: %v, want left=%d", st, want)
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func TestDepositingQueueSurvivesARestart(t *testing.T) {
	b, pans := testBase(t, model.THMDep)
	d := newDep(b)
	d.enqueue(batch{id: "B0007", pans: 24, target: 450, tol: 5})
	d.enqueue(batch{id: "B0008", pans: 24, target: 450, tol: 5})
	for i := 0; i < 10; i++ { // ten pans of the first batch are filled
		d.consumed("B0007")
	}
	waitSaved(t, pans, 14)

	// the station restarts: a new process reads the saved queue
	b2 := &svc.Base{ID: model.THMDep, Ctx: b.Ctx, N: node.New(node.Config{ID: model.THMDep}), F: sim.NewFaults(), Pans: pans}
	d2 := newDep(b2)
	d2.restoreQueue()
	if len(d2.batters) != 2 {
		t.Fatalf("both batches come back, got %d", len(d2.batters))
	}
	first := <-d2.batters
	if first.id != "B0007" || first.pans != 14 || first.target != 450 || first.tol != 5 {
		t.Fatalf("the first batch continues with the pans still to fill: %+v", first)
	}
	if second := <-d2.batters; second.id != "B0008" || second.pans != 24 {
		t.Fatalf("second batch: %+v", second)
	}
	if !d2.seenBatch["B0007"] {
		t.Fatal("a batch that comes back must not be accepted again when inspection resends it")
	}
}

func TestFinishedBatchLeavesTheSavedQueue(t *testing.T) {
	b, pans := testBase(t, model.THMDep)
	d := newDep(b)
	d.enqueue(batch{id: "B0007", pans: 2, target: 450, tol: 5})
	d.consumed("B0007")
	d.consumed("B0007")
	deadline := time.After(2 * time.Second)
	for {
		if st, _ := pans.LoadState(context.Background(), queueKey); len(st) == 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("a finished batch must not stay in the saved queue")
		case <-time.After(10 * time.Millisecond):
		}
	}
	b2 := &svc.Base{ID: model.THMDep, Ctx: b.Ctx, N: node.New(node.Config{ID: model.THMDep}), F: sim.NewFaults(), Pans: pans}
	d2 := newDep(b2)
	d2.restoreQueue()
	if len(d2.batters) != 0 {
		t.Fatal("nothing to take up after a finished batch")
	}
}
