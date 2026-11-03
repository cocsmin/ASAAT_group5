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
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"cakefactory/internal/model"
	"cakefactory/internal/sig"
	"cakefactory/internal/svc"
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

// jobQueue keeps the batches a station has received and not finished in the document store, so
// that a restart of the station does not lose them: it takes them up again and works on them.
// (Before, dosing waited two minutes for a batch that a restarted mixing or inspection station
// had forgotten.) The order id is not saved: it is "PO-" plus the number of the batch id.
type jobQueue struct {
	b      *svc.Base
	key    string
	mu     sync.Mutex
	jobs   map[string]job
	saveMu sync.Mutex
}

func newJobQueue(b *svc.Base, key string) *jobQueue {
	return &jobQueue{b: b, key: key, jobs: map[string]job{}}
}

// add notes a received batch.
func (q *jobQueue) add(j job) {
	q.mu.Lock()
	q.jobs[j.batch] = j
	q.mu.Unlock()
	q.save()
}

// done forgets a batch that has been passed on or discarded.
func (q *jobQueue) done(batch string) {
	q.mu.Lock()
	delete(q.jobs, batch)
	q.mu.Unlock()
	q.save()
}

func (q *jobQueue) save() {
	go func() {
		q.saveMu.Lock() // one write at a time, each takes the queue as it is now: the last write is the newest
		defer q.saveMu.Unlock()
		q.mu.Lock()
		m := map[string]int64{}
		for id, j := range q.jobs {
			m[id+".pans"] = int64(j.pans)
			m[id+".mix"] = int64(j.mixSeconds)
			m[id+".target10"] = int64(j.fillTarget*10 + 0.5)
			m[id+".tol10"] = int64(j.fillTol*10 + 0.5)
		}
		q.mu.Unlock()
		_ = q.b.Pans.SaveState(q.b.Ctx, q.key, m)
	}()
}

// restore returns the batches that were waiting when the station stopped, oldest first.
func (q *jobQueue) restore() []job {
	st, err := q.b.Pans.LoadState(q.b.Ctx, q.key)
	if err != nil || len(st) == 0 {
		return nil
	}
	var ids []string
	for k := range st {
		if id, ok := strings.CutSuffix(k, ".pans"); ok {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	var out []job
	for _, id := range ids {
		j := job{batch: id, order: "PO-" + strings.TrimPrefix(id, "B"), pans: int(st[id+".pans"]), mixSeconds: int(st[id+".mix"]),
			fillTarget: float64(st[id+".target10"]) / 10, fillTol: float64(st[id+".tol10"]) / 10}
		q.jobs[id] = j
		out = append(out, j)
	}
	if len(out) > 0 {
		log.Printf("[%s] %d batches taken up again from the saved queue", q.b.ID, len(out))
	}
	return out
}
