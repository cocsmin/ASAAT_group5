package bat

import (
	"testing"

	"cakefactory/internal/model"
	"cakefactory/internal/plm"
	"cakefactory/internal/sig"
	"cakefactory/internal/store"
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
