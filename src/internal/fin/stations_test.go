package fin

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

func put(t *testing.T, pans store.Pans, id, pan, stationName, status string, age time.Duration) {
	t.Helper()
	err := pans.Save(context.Background(), model.Pan{ID: id, PanID: pan, BatchID: "B0001", Station: stationName, Status: status,
		CakeTemp: 24, Updated: time.Now().Add(-age)})
	if err != nil {
		t.Fatal(err)
	}
}

func taken(set *station.PanSet, key string) bool { return !set.Add(key) }

func TestCakeDocumentIdNamesBatchAndPan(t *testing.T) {
	if got := (cake{pan: "PAN-007", batch: "B0042"}).doc(); got != "CAKE-B0042-PAN-007" {
		t.Fatalf("doc id %q", got)
	}
}

func TestDepanningAdoptsCooledCakesOnly(t *testing.T) {
	b, pans := testBase(t, model.FINDpn)
	s := &Dpn{Base: b, sec: station.NewSection(4), took: station.NewPanSet(), cooled: make(chan cake, 16)}
	put(t, pans, "PAN-001", "", "THM-Cooling", "cooled", 10*time.Second)
	put(t, pans, "PAN-002", "", "THM-Cooling", "cooled", 0)
	put(t, pans, "PAN-003", "", "THM-Cooling", "cooling", 10*time.Second)
	put(t, pans, "CAKE-B0001-PAN-009", "PAN-009", "THM-Cooling", "cooled", 10*time.Second) // a cake document is never a pan to depan
	s.adopt()
	if len(s.cooled) != 1 || !taken(s.took, "B0001/PAN-001") {
		t.Fatalf("only the old cooled pan is adopted, queue has %d", len(s.cooled))
	}
	if c := <-s.cooled; c.pan != "PAN-001" || c.batch != "B0001" {
		t.Fatalf("adopted cake: %+v", c)
	}
}

func TestInspectionAdoptsCakesAwaitingInspection(t *testing.T) {
	b, pans := testBase(t, model.FINQua)
	s := &Qua{Base: b, sec: station.NewSection(4), took: station.NewPanSet(), toInspect: make(chan cake, 16)}
	put(t, pans, "CAKE-B0001-PAN-001", "PAN-001", "FIN-Depanning", "awaiting inspection", 10*time.Second)
	put(t, pans, "CAKE-B0001-PAN-002", "PAN-002", "FIN-Depanning", "awaiting inspection", 0)
	put(t, pans, "CAKE-B0001-PAN-003", "PAN-003", "FIN-Inspection", "inspected", 10*time.Second)
	put(t, pans, "PAN-004", "", "FIN-Depanning", "awaiting inspection", 10*time.Second)
	s.adopt()
	if len(s.toInspect) != 1 || !taken(s.took, "CAKE-B0001-PAN-001") {
		t.Fatalf("only the old cake that awaits inspection is adopted, queue has %d", len(s.toInspect))
	}
}

func TestWrappingAdoptsInspectedCakes(t *testing.T) {
	b, pans := testBase(t, model.FINWrap)
	s := &Wrap{Base: b, sec: station.NewSection(5), took: station.NewPanSet(), toWrap: make(chan cake, 16), adm: &station.Admission{B: b}}
	put(t, pans, "CAKE-B0001-PAN-001", "PAN-001", "FIN-Inspection", "inspected", 10*time.Second)
	put(t, pans, "CAKE-B0001-PAN-002", "PAN-002", "FIN-Inspection", "rejected", 10*time.Second)
	put(t, pans, "CAKE-B0001-PAN-003", "PAN-003", "FIN-Inspection", "inspected", 0)
	s.adopt()
	if len(s.toWrap) != 1 || !taken(s.took, "CAKE-B0001-PAN-001") {
		t.Fatalf("only the old inspected cake is adopted, holding area has %d", len(s.toWrap))
	}
}

func TestPalletStationAdoptsWrappedCakes(t *testing.T) {
	b, pans := testBase(t, model.FINPal)
	s := &Pal{Base: b, sec: station.NewSection(5), took: station.NewPanSet(), toPallet: make(chan cake, 16), palletSize: 30}
	put(t, pans, "CAKE-B0001-PAN-001", "PAN-001", "FIN-Wrapping", "wrapped", 10*time.Second)
	put(t, pans, "CAKE-B0001-PAN-002", "PAN-002", "FIN-Wrapping", "refused", 10*time.Second)
	put(t, pans, "CAKE-B0001-PAN-003", "PAN-003", "FIN-Pallet", "packed", 10*time.Second)
	s.adopt()
	if len(s.toPallet) != 1 || !taken(s.took, "CAKE-B0001-PAN-001") {
		t.Fatalf("only the wrapped cake is adopted, queue has %d", len(s.toPallet))
	}
}

func TestWashingAdoptsEmptyPansFromDepanningAndFromDivertingStations(t *testing.T) {
	b, pans := testBase(t, model.FINWash)
	s := &Wash{Base: b, took: station.NewPanSet(), washQ: make(chan string, 16), panReq: make(chan struct{}, 4), adm: &station.Admission{B: b}}
	put(t, pans, "PAN-001", "", "FIN-Depanning", "depanned", 10*time.Second)
	put(t, pans, "PAN-002", "", "FIN-Depanning", "emptied", 10*time.Second)
	put(t, pans, "PAN-003", "", "THM", "discarded", 10*time.Second)
	put(t, pans, "PAN-004", "", "FIN-Washing", "discarded", 10*time.Second) // a damaged pan, out of the loop
	put(t, pans, "PAN-005", "", "FIN-Depanning", "depanned", 0)
	put(t, pans, "PAN-006", "", "FIN-PanPool", "clean", time.Hour)
	s.adopt()
	if len(s.washQ) != 3 {
		t.Fatalf("three empty pans are adopted, queue has %d", len(s.washQ))
	}
	for _, id := range []string{"PAN-004", "PAN-005", "PAN-006"} {
		if taken(s.took, id) {
			t.Errorf("%s must not be adopted", id)
		}
	}
}

func TestWashingSeedsThePoolAndKeepsExistingPans(t *testing.T) {
	b, pans := testBase(t, model.FINWash)
	s := &Wash{Base: b, took: station.NewPanSet(), washQ: make(chan string, 16), panReq: make(chan struct{}, 4), adm: &station.Admission{B: b}}
	put(t, pans, "PAN-001", "", "FIN-PanPool", "clean", time.Minute)
	put(t, pans, "PAN-002", "", "THM-Baking", "baking", time.Minute)
	s.seedPans("5")
	all, _ := pans.List(context.Background(), 100)
	if len(all) != 5 {
		t.Fatalf("the loop has %d pans, want 5", len(all))
	}
	// PAN-001 was clean already, PAN-003 to PAN-005 are new; PAN-002 is on the line
	if len(s.pool) != 4 {
		t.Fatalf("pool has %d pans, want 4 (every pan but the one on the line): %v", len(s.pool), s.pool)
	}
	for _, id := range s.pool {
		if id == "PAN-002" {
			t.Fatal("a pan that is baking must not be in the clean pool")
		}
	}
}

func TestWashingEnqueueDropsDuplicatesAndEmptyIds(t *testing.T) {
	b, _ := testBase(t, model.FINWash)
	s := &Wash{Base: b, took: station.NewPanSet(), washQ: make(chan string, 16), panReq: make(chan struct{}, 4), adm: &station.Admission{B: b}}
	s.enqueue("PAN-001")
	s.enqueue("PAN-001")
	s.enqueue("")
	if len(s.washQ) != 1 {
		t.Fatalf("a pan is washed once however many messages name it, queue has %d", len(s.washQ))
	}
}

func TestMachineIdsOfTheStationsMatchTheirOwnNodes(t *testing.T) {
	for machine, node := range map[string]string{depannerMachine: model.FINDpn, inspectorMachine: model.FINQua,
		wrapperMachine: model.FINWrap, washerMachine: model.FINWash} {
		if got := model.NodeOfMachine(machine, ""); got != node {
			t.Errorf("%s belongs to %s, the ticket would go to %q", machine, node, got)
		}
	}
}

func TestHoldingAreaIsSmallerThanAPallet(t *testing.T) {
	if holdingCap >= 30 {
		t.Fatal("the holding area in front of wrapping is a short queue, not a pallet")
	}
}
