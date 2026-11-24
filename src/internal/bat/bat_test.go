package bat

import (
	"context"
	"testing"
	"time"

	"cakefactory/internal/model"
	"cakefactory/internal/node"
	"cakefactory/internal/plm"
	"cakefactory/internal/sig"
	"cakefactory/internal/store"
	"cakefactory/internal/svc"
)

func TestJobSurvivesTheTripOverTheBus(t *testing.T) {
	j := job{batch: "B0007-2", order: "PO-0007", mixSeconds: 6, pans: 30, fillTarget: 450, fillTol: 5}
	// what the sending station puts on the wire and the receiving station reads back
	s := sig.New(sig.BatchToMix, j.fields()...)
	got := jobOf(sig.FromMetrics(s.Metrics())[0])
	if got != j {
		t.Fatalf("job changed on the way: sent %+v, received %+v", j, got)
	}
}

func TestFieldsCanCarryExtraValues(t *testing.T) {
	j := job{batch: "B1"}
	f := j.fields("result", "handed")
	if f[len(f)-2] != "result" || f[len(f)-1] != "handed" {
		t.Fatalf("extra fields are appended: %v", f)
	}
}

func TestIngredientListRoundTrip(t *testing.T) {
	items := []model.Item{{Ingredient: "flour", Grams: 5000}, {Ingredient: "milk", Grams: 1250.5}}
	got := plm.DecodeItems(encode(items))
	if len(got) != 2 || got[0] != items[0] || got[1] != items[1] {
		t.Fatalf("round trip: %+v", got)
	}
	if len(plm.DecodeItems("")) != 0 {
		t.Fatal("an empty list decodes to nothing")
	}
}

func TestSeedRecipeEncodesAllIngredients(t *testing.T) {
	got := plm.DecodeItems(encode(store.SeedRecipe.Items))
	if len(got) != len(store.SeedRecipe.Items) {
		t.Fatalf("lost ingredients: %+v", got)
	}
}

func TestBatchLostTimeoutIsLongerThanTheWholeBatterPath(t *testing.T) {
	// mixing 6 s + inspection 3 s + the longest wait for depositing (P3): dosing must not give up before that
	path := float64(store.SeedRecipe.MixSeconds) + inspectionTime.Seconds() + maxBatterWait.Seconds()
	if batchLostAfter.Seconds() <= path {
		t.Fatalf("a batch is given up after %.0f s but may legitimately need %.0f s", batchLostAfter.Seconds(), path)
	}
}

func TestNormalWeighingAlmostNeverFailsTheDoseTolerance(t *testing.T) {
	// a failure case must be an injected one: the tolerance is at least 3.5 standard deviations of the load cell
	if store.SeedRecipe.DoseTolPct < 3.5*loadCellScatter*100 {
		t.Fatalf("dose tolerance %.2f %% is too tight for a load cell that scatters %.2f %%", store.SeedRecipe.DoseTolPct, loadCellScatter*100)
	}
}

func queueBase(t *testing.T) *svc.Base {
	t.Helper()
	pans, err := store.NewFilePans(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { time.Sleep(200 * time.Millisecond) })
	t.Cleanup(cancel)
	return &svc.Base{ID: model.BATMix, Ctx: ctx, N: node.New(node.Config{ID: model.BATMix}), Pans: pans}
}

func waitQueue(t *testing.T, b *svc.Base, key string, want int) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		if st, _ := b.Pans.LoadState(context.Background(), key); len(st) == want {
			return
		}
		select {
		case <-deadline:
			st, _ := b.Pans.LoadState(context.Background(), key)
			t.Fatalf("saved queue has %d values, want %d: %v", len(st), want, st)
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func TestBatchesSurviveARestartOfMixingOrInspection(t *testing.T) {
	b := queueBase(t)
	q := newJobQueue(b, "BAT-MIX.queue")
	q.add(job{batch: "B0007", order: "PO-0007", mixSeconds: 6, pans: 24, fillTarget: 450, fillTol: 5})
	q.add(job{batch: "B0008-2", order: "PO-0008-2", mixSeconds: 6, pans: 24, fillTarget: 450, fillTol: 5})
	waitQueue(t, b, "BAT-MIX.queue", 8)

	again := newJobQueue(b, "BAT-MIX.queue") // the station restarts
	got := again.restore()
	if len(got) != 2 || got[0].batch != "B0007" || got[1].batch != "B0008-2" {
		t.Fatalf("both batches come back in order: %+v", got)
	}
	if got[0].order != "PO-0007" || got[1].order != "PO-0008-2" {
		t.Fatalf("the order id follows from the batch id, also after a restarted batch: %+v", got)
	}
	if got[0].pans != 24 || got[0].mixSeconds != 6 || got[0].fillTarget != 450 || got[0].fillTol != 5 {
		t.Fatalf("the batch keeps its targets: %+v", got[0])
	}
}

func TestFinishedBatchLeavesTheQueue(t *testing.T) {
	b := queueBase(t)
	q := newJobQueue(b, "BAT-INSP.queue")
	q.add(job{batch: "B0007", pans: 24, mixSeconds: 6, fillTarget: 450, fillTol: 5})
	q.add(job{batch: "B0008", pans: 24, mixSeconds: 6, fillTarget: 450, fillTol: 5})
	q.done("B0007")
	waitQueue(t, b, "BAT-INSP.queue", 4)
	got := newJobQueue(b, "BAT-INSP.queue").restore()
	if len(got) != 1 || got[0].batch != "B0008" {
		t.Fatalf("only the unfinished batch comes back: %+v", got)
	}
}

func TestEmptyQueueRestoresNothing(t *testing.T) {
	if got := newJobQueue(queueBase(t), "BAT-MIX.queue").restore(); len(got) != 0 {
		t.Fatalf("nothing saved, nothing restored: %+v", got)
	}
}
