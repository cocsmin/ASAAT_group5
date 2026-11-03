package bat

import (
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"cakefactory/internal/model"
	"cakefactory/internal/plm"
	"cakefactory/internal/sig"
	"cakefactory/internal/sim"
	"cakefactory/internal/svc"
	"cakefactory/internal/timing"
)

// loadCellScatter is the standard deviation of a weighing, as a fraction of the weight.
const loadCellScatter = 0.0025

// Dose is the dosing station. It takes the production orders, reserves the
// ingredients, doses and weighs them, hands the batch to mixing and waits for the
// result before it starts the next one (UC3, FC1 to FC5).
type Dose struct {
	*svc.Base
	orders     chan order
	results    chan sig.Signal // ingredient reservation results from PLM
	batchDone  chan sig.Signal // "Batch result" from mixing or inspection
	depStopped atomic.Bool
	failedRun  int // consecutive failed dose measurements
	seenMu     sync.Mutex
	seen       map[string]bool // order ids already received
}

// RunDose starts the dosing station.
func RunDose() {
	b, cancel := svc.Run(model.BATDose, "Doser", "LoadCell")
	defer cancel()
	t := &Dose{Base: b, orders: make(chan order, 256), results: make(chan sig.Signal, 256), batchDone: make(chan sig.Signal, 256), seen: map[string]bool{}}
	t.N.OnSignal(model.PLMWh, sig.OrderAndRecipe, func(_ string, s sig.Signal) {
		t.seenMu.Lock()
		dup := t.seen[s.Str("orderId")]
		t.seen[s.Str("orderId")] = true
		t.seenMu.Unlock()
		if dup { // PLM sends an order again when it was not picked up
			return
		}
		t.orders <- order{
			id: s.Str("orderId"), batch: s.Str("batchId"), recipe: s.Str("recipe"),
			items: plm.DecodeItems(s.Str("items")), mixSeconds: s.Int("mixSeconds"), tolPct: s.Float("doseTolPct"),
			pans: s.Int("pansPerBatch"), fillTarget: s.Float("fillTarget"), fillTol: s.Float("fillTol"),
		}
	})
	t.N.OnSignal(model.PLMWh, sig.IngredientReq, func(_ string, s sig.Signal) {
		if !s.Bool("request") {
			t.results <- s
		}
	})
	t.N.OnSignal("", sig.BatchResult, func(_ string, s sig.Signal) { t.batchDone <- s })
	t.N.OnSignal(model.THMDep, sig.DepositingState, func(_ string, s sig.Signal) {
		if t.depStopped.Swap(s.Bool("stopped")) == s.Bool("stopped") {
			return // state signals are repeated with every heartbeat
		}
		if s.Bool("stopped") {
			t.N.Warn("FC7", "depositing stopped: holding new batches", "")
		} else {
			t.N.Log("FC7", "depositing working again: batches resume", "")
		}
	})
	t.Start()
	t.N.Gauge(stageDosing, 0)
	t.loop()
}

func (t *Dose) loop() {
	for {
		select {
		case <-t.Ctx.Done():
			return
		case o := <-t.orders:
			t.process(o)
		}
	}
}

// ingredients waits for the reservation result of an order. It sends the
// request first when asked and repeats it when PLM does not answer within
// 20 s (PLM may have restarted). PLM answers a repeated request idempotently.
func (t *Dose) ingredients(o order, sendFirst bool) (sig.Signal, bool) {
	send := func() {
		t.N.SendReliable(sig.New(sig.IngredientReq, "request", true, "orderId", o.id, "batchId", o.batch,
			"items", encode(o.items)))
	}
	if sendFirst {
		send()
	}
	for {
		select {
		case <-t.Ctx.Done():
			return sig.Signal{}, false
		case r := <-t.results:
			if r.Str("orderId") == o.id {
				return r, true
			}
		case <-time.After(sim.Scale(timing.IngredientRetry)):
			t.N.Warn("FC2", "no answer from production management: ingredient request repeated", o.batch)
			send()
		}
	}
}

// reserve asks production management for the ingredients of a batch and waits
// for the confirmation (Dosing state machine: Reserving). Dosing does not start
// before the request is confirmed.
func (t *Dose) reserve(o order) bool {
	r, ok := t.ingredients(o, true)
	if !ok {
		return false
	}
	for r.Str("result") == "insufficient" {
		t.N.SetDevice("Doser", model.StateIdle, "waiting for stock")
		t.N.Warn("FC1", "not enough ingredients: mixing does not start ("+r.Str("reason")+")", o.batch)
		if r, ok = t.ingredients(o, false); !ok {
			return false
		}
	}
	if r.Str("result") == "staged" {
		t.N.Log("FC2", "warehouse unavailable: continuing with ingredients already staged", o.batch)
	}
	t.N.SetDevice("Doser", model.StateOK, "")
	return true
}

func (t *Dose) process(o order) {
	if !t.reserve(o) {
		return
	}
	for attempt := 1; ; attempt++ {
		batch := o.batch
		if attempt > 1 {
			batch = fmt.Sprintf("%s-%d", o.batch, attempt)
		}
		if attempt > 1 { // a discarded batch used up its ingredients: reserve again for the new batch
			o2 := o
			o2.id, o2.batch = fmt.Sprintf("%s-%d", o.id, attempt), batch
			if !t.reserve(o2) {
				return
			}
		}
		// Hold new batches while depositing is stopped.
		for t.depStopped.Load() || !t.N.HostOnline() { // no production without the monitor
			if !sim.Sleep(t.Ctx, time.Second) {
				return
			}
		}
		done, cont := t.makeBatch(o, batch)
		if !cont {
			return
		}
		if done {
			t.N.AddGauge("batches.handedOver", 1)
			return
		}
		t.N.AddGauge("batches.discarded", 1)
	}
}

// makeBatch doses the batch, hands it to mixing and waits for the result. done is
// true when the batter was handed over to depositing, false when the batch was
// discarded (a new one is started). cont is false when the service is shutting down.
func (t *Dose) makeBatch(o order, batch string) (done, cont bool) {
	if t.dose(o, batch) != nil {
		return false, t.Ctx.Err() == nil
	}
	for len(t.batchDone) > 0 { // results of earlier, given-up batches
		<-t.batchDone
	}
	t.N.SendReliable(sig.New(sig.BatchToMix, job{batch: batch, order: o.id, mixSeconds: o.mixSeconds, pans: o.pans,
		fillTarget: o.fillTarget, fillTol: o.fillTol}.fields()...))
	lost := time.After(sim.Scale(batchLostAfter))
	for {
		select {
		case <-t.Ctx.Done():
			return false, false
		case r := <-t.batchDone:
			if r.Str("batchId") != batch {
				continue
			}
			return r.Str("result") == "handed", true
		case <-lost:
			t.N.Warn("FC27", "no result for batch "+batch+" from mixing or inspection (a station restarted?): batch given up", batch)
			return false, true
		}
	}
}

type dosed struct{ error }

// dose dispenses and weighs every ingredient (UC3, FC3, FC4, FC5).
func (t *Dose) dose(o order, batch string) error {
	t.N.Gauge(stageDosing, 1)
	defer t.N.Gauge(stageDosing, 0)
	t.failedRun = 0
	for _, it := range o.items {
		first := true
		for {
			if !sim.Sleep(t.Ctx, doseTime) {
				return t.Ctx.Err()
			}
			measured := it.Grams * (1 + sim.Noise(loadCellScatter))
			switch {
			case t.F.Take("FC5"):
				measured = it.Grams * 0.9 // the doser keeps under-dosing
			case first && t.F.Take("FC3"):
				measured = it.Grams * 0.85
			case first && t.F.Take("FC4"):
				measured = it.Grams * 1.2
			}
			first = false
			dev := (measured - it.Grams) / it.Grams * 100
			switch {
			case math.Abs(dev) <= o.tolPct:
				t.failedRun = 0
				t.N.SetDevice("LoadCell", model.StateOK, "")
			case dev < 0:
				t.failedRun++
				t.N.SetDevice("LoadCell", model.StateWarn, "under-dose")
				if t.failedRun >= maxFailedDoses {
					return t.lockout(o, batch, it.Ingredient)
				}
				t.N.Warn("FC3", fmt.Sprintf("%s under-dosed (%.0f g of %.0f g): requesting missing amount", it.Ingredient, measured, it.Grams), batch)
				sim.Sleep(t.Ctx, time.Second) // add the missing amount, then weigh again
				continue
			default:
				t.failedRun++
				t.N.SetDevice("LoadCell", model.StateWarn, "over-dose")
				t.N.Discard("FC4", fmt.Sprintf("%s over-dosed (%.0f g of %.0f g): batch discarded", it.Ingredient, measured, it.Grams), batch)
				if t.failedRun >= maxFailedDoses {
					return t.lockout(o, batch, it.Ingredient)
				}
				return dosed{fmt.Errorf("over-dose")}
			}
			break
		}
	}
	t.N.Log("UC3", "batch loading complete, all ingredients within tolerance", batch)
	return nil
}

// lockout stops dosing after repeated failed measurements (FC5) and waits for
// Maintenance.
func (t *Dose) lockout(o order, batch, ing string) error {
	t.F.Break(doserMachine)
	t.N.SetDevice("Doser", model.StateFault, "locked out after repeated dose failures")
	t.N.Discard("FC5", "batch discarded: dosing stopped", batch)
	t.N.Fault("FC5", fmt.Sprintf("dosing failed %d times in a row on %s: doser locked out", t.failedRun, ing), batch, "Doser", doserMachine)
	if !t.WaitRepaired(doserMachine) {
		return t.Ctx.Err()
	}
	t.failedRun = 0
	t.N.SetDevice("Doser", model.StateOK, "")
	t.N.SetDevice("LoadCell", model.StateOK, "")
	t.N.Log("FC5", "doser repaired: dosing resumes", batch)
	return dosed{fmt.Errorf("restart after repair")}
}
