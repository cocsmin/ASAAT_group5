package bat

import (
	"fmt"
	"sync/atomic"
	"time"

	"cakefactory/internal/model"
	"cakefactory/internal/sig"
	"cakefactory/internal/sim"
	"cakefactory/internal/svc"
)

// Insp is the inspection station: the human inspection gate and the batter handoff
// to thermal processing (UC4, FC7).
type Insp struct {
	*svc.Base
	jobs       chan job
	depStopped atomic.Bool
}

// RunInsp starts the inspection station.
func RunInsp() {
	b, cancel := svc.Run(model.BATInsp, "InspectionStation")
	defer cancel()
	t := &Insp{Base: b, jobs: make(chan job, 256)}
	t.N.OnSignal(model.BATMix, sig.BatterToInspect, func(_ string, s sig.Signal) { t.jobs <- jobOf(s) })
	t.N.OnSignal(model.THMDep, sig.DepositingState, func(_ string, s sig.Signal) { t.depStopped.Store(s.Bool("stopped")) })
	t.Start()
	t.N.Gauge(stageInspection, 0)
	t.N.Gauge(stageWaiting, 0)
	for {
		select {
		case <-t.Ctx.Done():
			return
		case j := <-t.jobs:
			t.inspect(j)
		}
	}
}

func (t *Insp) inspect(j job) {
	t.N.Gauge(stageInspection, 1)
	t.N.SetDevice("InspectionStation", model.StateOK, "inspector checking batter")
	sim.Sleep(t.Ctx, inspectionTime)
	t.N.Gauge(stageInspection, 0)
	t.N.SetDevice("InspectionStation", model.StateOK, "")
	if t.F.Take("UC4") {
		t.N.SendReliable(sig.New(sig.BatterRejected, j.fields()...))
		return
	}
	t.N.Log("UC4", "inspector marked batter good", j.batch)
	t.handOff(j)
}

// handOff signals the batter to Thermal Processing, holding it while depositing
// is stopped and discarding it once it has waited longer than P3 (FC7).
func (t *Insp) handOff(j job) {
	waited := time.Duration(0)
	for t.depStopped.Load() {
		t.N.Gauge(stageWaiting, 1)
		if !sim.Sleep(t.Ctx, time.Second) {
			return
		}
		waited += time.Second
		if waited > maxBatterWait {
			t.N.Gauge(stageWaiting, 0)
			t.N.Discard("FC7", fmt.Sprintf("batter waited %.0f s (limit %.0f s): discarded", waited.Seconds(), maxBatterWait.Seconds()), j.batch)
			t.N.SendReliable(sig.New(sig.BatchResult, "batchId", j.batch, "result", "discarded"))
			return
		}
	}
	t.N.Gauge(stageWaiting, 0)
	t.N.SendReliable(sig.New(sig.BatterReady, "batchId", j.batch, "orderId", j.order, "pans", int64(j.pans),
		"fillTarget", j.fillTarget, "fillTol", j.fillTol))
	t.N.Log("UC4", fmt.Sprintf("batter ready: %d pans worth handed to thermal processing", j.pans), j.batch)
	t.N.SendReliable(sig.New(sig.BatchResult, "batchId", j.batch, "result", "handed"))
}
