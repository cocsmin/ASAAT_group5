// Package bat is the Batter Preparation system, split per station, one container
// each: Dosing (dose.go: order intake, ingredient reservation, dosing), Mixing
// (mix.go) and Inspection (insp.go: the human inspection gate and the handoff to
// depositing). A batch travels with signals: "Batch to mixing" (dosing to mixing),
// "Batter to inspection" (mixing to inspection), "Batter rejected" (inspection back to
// mixing: another mixing cycle) and "Batch result" (mixing or inspection to dosing:
// handed over or discarded). Dosing makes one batch at a time and treats a batch whose
// result does not arrive in time as lost (a station that restarts forgets the batch
// it had, which BAT-NFR-02 allows).
package bat

import (
	"fmt"
	"strings"
	"time"

	"cakefactory/internal/model"
	"cakefactory/internal/sig"
	"cakefactory/internal/timing"
)

// Parameters (simulated time), see the BAT document.
const (
	maxFailedDoses  = 3                 // P2
	maxBatterWait   = timing.BatterWait // P3
	doseTime        = 1200 * time.Millisecond
	inspectionTime  = 3 * time.Second
	batchLostAfter  = 120 * time.Second // a batch whose result has not come back by now is given up
	doserMachine    = "bat-dose-doser"
	mixerMachine    = "bat-mix-mixer"
	stageDosing     = "stage.dosing"
	stageMixing     = "stage.mixing"
	stageInspection = "stage.inspection"
	stageWaiting    = "stage.waiting"
)

type order struct {
	id, batch, recipe string
	items             []model.Item
	mixSeconds        int
	tolPct            float64
	pans              int
	fillTarget        float64
	fillTol           float64
}

// job is what mixing and inspection need to know about a batch.
type job struct {
	batch, order string
	mixSeconds   int
	pans         int
	fillTarget   float64
	fillTol      float64
}

func (j job) fields(kv ...any) []any {
	return append([]any{"batchId", j.batch, "orderId", j.order, "mixSeconds", int64(j.mixSeconds), "pans", int64(j.pans),
		"fillTarget", j.fillTarget, "fillTol", j.fillTol}, kv...)
}

func jobOf(s sig.Signal) job {
	return job{batch: s.Str("batchId"), order: s.Str("orderId"), mixSeconds: s.Int("mixSeconds"), pans: s.Int("pans"),
		fillTarget: s.Float("fillTarget"), fillTol: s.Float("fillTol")}
}

func encode(items []model.Item) string {
	parts := make([]string, len(items))
	for i, it := range items {
		parts[i] = fmt.Sprintf("%s:%g", it.Ingredient, it.Grams)
	}
	return strings.Join(parts, ";")
}
