package store

import (
	"context"
	"testing"
	"time"

	"cakefactory/internal/model"
)

func memSQL(t *testing.T) *MemSQL {
	t.Helper()
	m := NewMemSQL()
	if err := m.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	return m
}

var ctx = context.Background()

func TestSeedRecipeMatchesTheBatch(t *testing.T) {
	// one batch fills PansPerBatch pans of FillTarget grams: the ingredients must add up to about that
	var grams float64
	for _, it := range SeedRecipe.Items {
		grams += it.Grams
	}
	batter := float64(SeedRecipe.PansPerBatch) * SeedRecipe.FillTarget
	if grams < 0.8*batter || grams > 1.2*batter {
		t.Fatalf("ingredients %.0f g do not fit %d pans of %.0f g (%.0f g)", grams, SeedRecipe.PansPerBatch, SeedRecipe.FillTarget, batter)
	}
	if SeedStockGrams < 5*SeedRecipe.Items[0].Grams {
		t.Fatal("the start stock must cover several batches")
	}
}

func TestReserveHoldsStockAndIsRepeatable(t *testing.T) {
	m := memSQL(t)
	items := []model.Item{{Ingredient: "flour", Grams: 5000}}
	ok, _, err := m.Reserve(ctx, "PO-0001", items)
	if err != nil || !ok {
		t.Fatalf("reserve: %v %v", ok, err)
	}
	if ok, _, _ = m.Reserve(ctx, "PO-0001", items); !ok {
		t.Fatal("a repeated request for the same order gets the same answer")
	}
	rows, _ := m.Stock(ctx)
	for _, r := range rows {
		if r.Ingredient == "flour" && r.Reserved != 5000 {
			t.Fatalf("a repeated request must not reserve twice, reserved %.0f", r.Reserved)
		}
	}
}

func TestReserveRefusesWhenStockIsTooLow(t *testing.T) {
	m := memSQL(t)
	big := []model.Item{{Ingredient: "flour", Grams: SeedStockGrams + 1}}
	ok, reason, _ := m.Reserve(ctx, "PO-0001", big)
	if ok || reason == "" {
		t.Fatal("not enough flour must be refused with a reason")
	}
	if ok, reason, _ = m.Reserve(ctx, "PO-0002", []model.Item{{Ingredient: "saffron", Grams: 1}}); ok || reason == "" {
		t.Fatal("an unknown ingredient must be refused")
	}
	rows, _ := m.Stock(ctx)
	for _, r := range rows {
		if r.Reserved != 0 {
			t.Fatalf("a refused order must reserve nothing, %s has %.0f reserved", r.Ingredient, r.Reserved)
		}
	}
}

func TestConsumeTurnsTheHoldIntoAWithdrawal(t *testing.T) {
	m := memSQL(t)
	_, _, _ = m.Reserve(ctx, "PO-0001", []model.Item{{Ingredient: "eggs", Grams: 2000}})
	if err := m.Consume(ctx, "PO-0001"); err != nil {
		t.Fatal(err)
	}
	rows, _ := m.Stock(ctx)
	for _, r := range rows {
		if r.Ingredient == "eggs" && (r.Grams != SeedStockGrams-2000 || r.Reserved != 0) {
			t.Fatalf("after consume eggs = %.0f g, reserved %.0f", r.Grams, r.Reserved)
		}
	}
	if ok, _, _ := m.Reserve(ctx, "PO-0001", nil); !ok {
		t.Fatal("a consumed order stays answered")
	}
}

func TestRestockAddsStock(t *testing.T) {
	m := memSQL(t)
	_ = m.Restock(ctx, "milk", 1000)
	rows, _ := m.Stock(ctx)
	for _, r := range rows {
		if r.Ingredient == "milk" && r.Grams != SeedStockGrams+1000 {
			t.Fatalf("milk = %.0f", r.Grams)
		}
	}
}

func TestLastOrderNoContinuesAfterARestart(t *testing.T) {
	m := memSQL(t)
	if n, _ := m.LastOrderNo(ctx); n != 0 {
		t.Fatal("no orders yet")
	}
	for _, id := range []string{"PO-0003", "PO-0012", "PO-0007"} {
		_ = m.SaveOrder(ctx, model.Order{ID: id})
	}
	if n, _ := m.LastOrderNo(ctx); n != 12 {
		t.Fatalf("highest stored order is 12, got %d", n)
	}
}

func TestEventsNewestFirstAndDiscardCounts(t *testing.T) {
	m := memSQL(t)
	for i, sev := range []string{model.SevLog, model.SevDiscard, model.SevDiscard, model.SevWarning} {
		e := model.LogEvent{Node: model.THMExit, Severity: sev, Code: "FC12"}
		if i == 2 {
			e.Node = model.FINQua
		}
		_ = m.InsertEvent(ctx, &e)
		if e.ID != int64(i+1) {
			t.Fatalf("event ids count up, got %d", e.ID)
		}
	}
	ev, _ := m.Events(ctx, 2)
	if len(ev) != 2 || ev[0].ID != 4 || ev[1].ID != 3 {
		t.Fatalf("newest first and limited, got %+v", ev)
	}
	d, _ := m.DiscardCounts(ctx)
	if d[model.THMExit] != 1 || d[model.FINQua] != 1 || len(d) != 2 {
		t.Fatalf("discards by node: %v", d)
	}
}

func TestTicketsOpenAndClose(t *testing.T) {
	m := memSQL(t)
	a := model.Ticket{Node: model.THMBake, MachineID: "thm-bake-zone-2"}
	b := model.Ticket{Node: model.FINWash, MachineID: "fin-wash-washer"}
	_ = m.OpenTicket(ctx, &a)
	_ = m.OpenTicket(ctx, &b)
	if a.Status != "open" || a.ID == b.ID {
		t.Fatalf("tickets open with their own id: %+v %+v", a, b)
	}
	_ = m.CloseTicket(ctx, a.ID)
	tk, _ := m.Tickets(ctx, 10)
	if len(tk) != 2 || tk[0].ID != b.ID {
		t.Fatalf("newest first: %+v", tk)
	}
	for _, x := range tk {
		if x.ID == a.ID && (x.Status != "repaired" || x.Closed == nil) {
			t.Fatalf("closed ticket: %+v", x)
		}
		if x.ID == b.ID && x.Status != "open" {
			t.Fatal("the other ticket stays open")
		}
	}
}

func filePans(t *testing.T) *FilePans {
	t.Helper()
	f, err := NewFilePans(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestMoveRecordsTheJourneyOfAPan(t *testing.T) {
	f := filePans(t)
	Move(ctx, f, "PAN-001", "THM-Depositing", "filled", "447.9 g", func(p *model.Pan) { p.BatchID, p.FillWeight = "B0001", 447.9 })
	p := Move(ctx, f, "PAN-001", "THM-Baking", "baking", "", nil)
	if p.Station != "THM-Baking" || p.Status != "baking" || p.BatchID != "B0001" || p.FillWeight != 447.9 {
		t.Fatalf("pan after two moves: %+v", p)
	}
	if len(p.History) != 2 || p.History[0].Status != "filled" || p.History[1].Status != "baking" {
		t.Fatalf("history: %+v", p.History)
	}
	got, ok, _ := f.Get(ctx, "PAN-001")
	if !ok || got.Status != "baking" {
		t.Fatalf("stored: %+v %v", got, ok)
	}
}

func TestMoveKeepsOnlyTheLast40Steps(t *testing.T) {
	f := filePans(t)
	for i := 0; i < 55; i++ {
		Move(ctx, f, "PAN-001", "FIN-PanPool", "clean", "", nil)
	}
	p, _, _ := f.Get(ctx, "PAN-001")
	if len(p.History) != 40 {
		t.Fatalf("history is capped at 40 steps, has %d", len(p.History))
	}
}

func TestListReturnsTheNewestPansFirst(t *testing.T) {
	f := filePans(t)
	for i, id := range []string{"PAN-001", "PAN-002", "PAN-003"} {
		_ = f.Save(ctx, model.Pan{ID: id, Updated: time.Now().Add(time.Duration(i) * time.Minute)})
	}
	got, _ := f.List(ctx, 2)
	if len(got) != 2 || got[0].ID != "PAN-003" || got[1].ID != "PAN-002" {
		t.Fatalf("newest first and limited: %+v", got)
	}
	if _, ok, _ := f.Get(ctx, "PAN-404"); ok {
		t.Fatal("a missing pan is not an error and not found")
	}
}

func TestNodeStateRoundTrip(t *testing.T) {
	f := filePans(t)
	if st, err := f.LoadState(ctx, "THM-BAKE"); err != nil || st != nil {
		t.Fatalf("no saved state yet: %v %v", st, err)
	}
	_ = f.SaveState(ctx, "THM-BAKE", map[string]int64{"deposited": 12, "zone1": 160})
	st, _ := f.LoadState(ctx, "THM-BAKE")
	if st["deposited"] != 12 || st["zone1"] != 160 {
		t.Fatalf("state: %v", st)
	}
	if other, _ := f.LoadState(ctx, "THM-COOL"); other != nil {
		t.Fatal("state is per node")
	}
}
