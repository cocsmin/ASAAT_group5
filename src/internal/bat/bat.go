// Package bat is the Batter Preparation system: ingredient intake and dosing,
// mixing control, the human inspection gate and the batter handoff.
package bat

import (
	"fmt"
	"math"
	"strings"
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

// Parameters (simulated time), see the BAT document.
const (
	maxFailedDoses  = 3                 // P2
	maxBatterWait   = timing.BatterWait // P3
	doseTime        = 1200 * time.Millisecond
	inspectionTime  = 3 * time.Second
	doserMachine    = "bat-doser"
	mixerMachine    = "bat-mixer"
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

// BAT is the batter preparation service.
type BAT struct {
	*svc.Base
	orders     chan order
	results    chan sig.Signal
	depStopped atomic.Bool
	failedRun  int // consecutive failed dose measurements
	seenMu     sync.Mutex
	seen       map[string]bool // order ids already received
}

// Run starts the service.
func Run() {
	b, cancel := svc.Run(model.BAT, "Doser", "LoadCell", "Mixer", "ConsistencySensor", "InspectionStation")
	defer cancel()
	t := &BAT{Base: b, orders: make(chan order, 256), results: make(chan sig.Signal, 256), seen: map[string]bool{}}
	t.N.OnSignal(model.PLM, sig.OrderAndRecipe, func(_ string, s sig.Signal) {
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
	t.N.OnSignal(model.PLM, sig.IngredientReq, func(_ string, s sig.Signal) {
		if !s.Bool("request") {
			t.results <- s
		}
	})
	t.N.OnSignal(model.THM, sig.DepositingState, func(_ string, s sig.Signal) {
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
	for _, g := range []string{stageDosing, stageMixing, stageInspection, stageWaiting} {
		t.N.Gauge(g, 0)
	}
	t.loop()
}

func (t *BAT) loop() {
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
func (t *BAT) ingredients(o order, sendFirst bool) (sig.Signal, bool) {
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
func (t *BAT) reserve(o order) bool {
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

func (t *BAT) process(o order) {
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

func encode(items []model.Item) string {
	parts := make([]string, len(items))
	for i, it := range items {
		parts[i] = fmt.Sprintf("%s:%g", it.Ingredient, it.Grams)
	}
	return strings.Join(parts, ";")
}

// makeBatch runs dosing, mixing and inspection. done is true when the batter
// was handed over, false when the batch was discarded (a new one is started).
// cont is false when the service is shutting down.
func (t *BAT) makeBatch(o order, batch string) (done, cont bool) {
	if t.dose(o, batch) != nil {
		return false, t.Ctx.Err() == nil
	}
	if err := t.mix(o, batch); err != nil {
		return false, t.Ctx.Err() == nil
	}
	return t.handOff(o, batch)
}

type dosed struct{ error }

// dose dispenses and weighs every ingredient (UC3, FC3, FC4, FC5).
func (t *BAT) dose(o order, batch string) error {
	t.N.Gauge(stageDosing, 1)
	defer t.N.Gauge(stageDosing, 0)
	t.failedRun = 0
	for _, it := range o.items {
		first := true
		for {
			if !sim.Sleep(t.Ctx, doseTime) {
				return t.Ctx.Err()
			}
			measured := it.Grams * (1 + sim.Noise(0.004))
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
func (t *BAT) lockout(o order, batch, ing string) error {
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

// mix runs the mixing cycle with consistency monitoring (UC4, FC6).
func (t *BAT) mix(o order, batch string) error {
	t.N.Gauge(stageMixing, 1)
	defer t.N.Gauge(stageMixing, 0)
	t.N.SetDevice("Mixer", model.StateOK, "mixing")
	defer func() {
		if !t.F.Broken(mixerMachine) {
			t.N.SetDevice("Mixer", model.StateOK, "")
		}
	}()
	for {
		d := time.Duration(o.mixSeconds) * time.Second
		step := d / 6
		for i := 0; i < 6; i++ {
			if !sim.Sleep(t.Ctx, step) {
				return t.Ctx.Err()
			}
			if t.F.Take("FC6") {
				t.F.Break(mixerMachine)
				t.N.SetDevice("Mixer", model.StateFault, "mixer broke")
				t.N.Discard("FC6", "mixer broke: batch in the mixer discarded", batch)
				t.N.Fault("FC6", "mixer broke during the mixing cycle", batch, "Mixer", mixerMachine)
				if !t.WaitRepaired(mixerMachine) {
					return t.Ctx.Err()
				}
				t.N.Log("FC6", "mixer repaired: mixing resumes", batch)
				return dosed{fmt.Errorf("restart after repair")}
			}
			t.N.SetDevice("ConsistencySensor", model.StateOK, fmt.Sprintf("viscosity %.1f", 60+float64(i)*3+sim.Noise(0.4)))
		}
		// Human inspection gate.
		t.N.Gauge(stageMixing, 0)
		t.N.Gauge(stageInspection, 1)
		t.N.SetDevice("InspectionStation", model.StateOK, "inspector checking batter")
		sim.Sleep(t.Ctx, inspectionTime)
		t.N.Gauge(stageInspection, 0)
		t.N.SetDevice("InspectionStation", model.StateOK, "")
		if t.F.Take("UC4") {
			t.N.Warn("UC4", "inspector marked batter not good: another mixing cycle starts immediately", batch)
			t.N.Gauge(stageMixing, 1)
			continue
		}
		t.N.Log("UC4", "inspector marked batter good", batch)
		return nil
	}
}

// handOff signals the batter to Thermal Processing, holding it while depositing
// is stopped and discarding it once it has waited longer than P3 (FC7).
func (t *BAT) handOff(o order, batch string) (done, cont bool) {
	waited := time.Duration(0)
	t.N.Gauge(stageWaiting, 0)
	for t.depStopped.Load() {
		t.N.Gauge(stageWaiting, 1)
		if !sim.Sleep(t.Ctx, time.Second) {
			return false, false
		}
		waited += time.Second
		if waited > maxBatterWait {
			t.N.Gauge(stageWaiting, 0)
			t.N.Discard("FC7", fmt.Sprintf("batter waited %.0f s (limit %.0f s): discarded", waited.Seconds(), maxBatterWait.Seconds()), batch)
			return false, true
		}
	}
	t.N.Gauge(stageWaiting, 0)
	t.N.SendReliable(sig.New(sig.BatterReady, "batchId", batch, "orderId", o.id, "pans", int64(o.pans),
		"fillTarget", o.fillTarget, "fillTol", o.fillTol))
	t.N.Log("UC4", fmt.Sprintf("batter ready: %d pans worth handed to thermal processing", o.pans), batch)
	return true, true
}
