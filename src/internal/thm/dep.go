package thm

import (
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"cakefactory/internal/model"
	"cakefactory/internal/sig"
	"cakefactory/internal/sim"
	"cakefactory/internal/station"
	"cakefactory/internal/store"
	"cakefactory/internal/svc"
)

// Dep is the depositing station: it fills clean pans with batter.
type Dep struct {
	*svc.Base
	sec     *station.Section
	batters chan batch
	clean   chan string
	pending atomic.Int64 // pan requests not answered yet

	cmu       sync.Mutex
	inClean   map[string]bool // clean pans waiting at depositing (drops duplicates)
	seenBatch map[string]bool

	mu       sync.Mutex
	reasons  map[string]string // why new pans are not admitted
	notified bool
	wrongRun int

	lastLowStock time.Time
	lowStock     atomic.Bool
}

// RunDep starts the depositing station.
func RunDep() {
	b, cancel := svc.Run(model.THMDep, "Depositor", "PanPresenceEye", "CheckWeigher")
	defer cancel()
	t := &Dep{Base: b, sec: station.NewSection(1), batters: make(chan batch, 1024), clean: make(chan string, 1024),
		reasons: map[string]string{}, inClean: map[string]bool{}, seenBatch: map[string]bool{}}
	t.N.OnSignal(model.BATInsp, sig.BatterReady, func(_ string, s sig.Signal) {
		t.cmu.Lock()
		dup := t.seenBatch[s.Str("batchId")]
		t.seenBatch[s.Str("batchId")] = true
		t.cmu.Unlock()
		if dup {
			return
		}
		t.batters <- batch{id: s.Str("batchId"), pans: s.Int("pans"), target: s.Float("fillTarget"), tol: s.Float("fillTol")}
		t.N.Gauge("queue", int64(len(t.batters)))
	})
	t.N.OnSignal(model.FINWash, sig.CleanPanSupply, func(_ string, s sig.Signal) {
		t.pending.Add(-1)
		t.lowStock.Store(s.Int("stock") <= 2)
		if id := s.Str("panId"); id != "" {
			t.addClean(id)
		}
		if s.Int("stock") <= 2 && time.Since(t.lastLowStock) > sim.Scale(20*time.Second) {
			t.lastLowStock = time.Now()
			t.N.Warn("FC23", fmt.Sprintf("clean pan stock low (%d): filling fewer pans", s.Int("stock")), "")
		}
	})
	// Stop / resume pan admission: from Finishing, and from the stations behind us.
	t.N.OnSignal("", sig.PanAdmission, func(from string, s sig.Signal) {
		switch from {
		case model.FINWrap:
			t.setReason("wrap", s.Str("reason"), s.Bool("stop"))
		case model.FINWash:
			t.setReason("wash", s.Str("reason"), s.Bool("stop"))
		case model.THMBake:
			t.setReason("zone", s.Str("reason"), s.Bool("stop"))
		case model.THMCool:
			t.setReason("buffer", s.Str("reason"), s.Bool("stop"))
		}
	})
	t.N.OnSignal(model.BT, sig.BandState, func(_ string, s sig.Signal) {
		halted := t.sec.OnBand(s) // the depositor works on section 1: a halted S1 stops it at once
		t.setReason("band", "band halted: no new pans", halted)
	})
	t.OnRepair(depositorMachine, func() {})
	// The report requires that production may not run without the monitor.
	t.setReason("monitor", "monitor offline: production may not run without it", true)
	t.N.OnHost(func(on bool) {
		t.setReason("monitor", "monitor offline: production may not run without it", !on)
		if !on {
			t.N.Warn("UC2", "monitor went offline: no new pans are admitted", "")
		}
	})
	t.restoreSupplied()
	t.Start()
	go t.panRefill()
	go t.gaugeLoop()
	t.depositor()
}

// setReason stops or resumes pan admission and tells batter preparation.
func (t *Dep) setReason(key, reason string, on bool) {
	t.mu.Lock()
	if on {
		t.reasons[key] = reason
	} else {
		delete(t.reasons, key)
	}
	stopped := len(t.reasons) > 0
	changed := stopped != t.notified
	t.notified = stopped
	why := make([]string, 0, len(t.reasons))
	for _, r := range t.reasons {
		why = append(why, r)
	}
	t.mu.Unlock()
	sort.Strings(why)
	if !changed {
		return
	}
	text := strings.Join(why, "; ")
	t.N.SetState(sig.New(sig.DepositingState, "stopped", stopped, "reason", text))
	if stopped {
		t.N.SetDevice("Depositor", model.StateWarn, "new pans stopped: "+text)
	} else {
		t.N.SetDevice("Depositor", model.StateOK, "")
		t.N.Log("FC7", "depositing admits new pans again", "")
	}
}

func (t *Dep) admitting() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.reasons) == 0
}

func (t *Dep) waitAdmission() bool {
	for !t.admitting() {
		if !sim.Sleep(t.Ctx, 300*time.Millisecond) {
			return false
		}
	}
	return true
}

func (t *Dep) requestPan() {
	t.pending.Add(1)
	t.N.SendReliable(sig.New(sig.PanRequest, "message", "waiting for pan"))
}

func (t *Dep) panRefill() {
	tick := time.NewTicker(sim.Scale(800 * time.Millisecond))
	defer tick.Stop()
	for {
		select {
		case <-t.Ctx.Done():
			return
		case <-tick.C:
			if len(t.clean)+int(t.pending.Load()) < 4 {
				t.requestPan()
			}
		}
	}
}

func (t *Dep) gaugeLoop() {
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-t.Ctx.Done():
			return
		case <-tick.C:
			t.N.Gauge("queue", int64(len(t.batters)))
		}
	}
}

func (t *Dep) depositor() {
	for {
		select {
		case <-t.Ctx.Done():
			return
		case b := <-t.batters:
			for i := 0; i < b.pans; i++ {
				if !t.depositOne(b) {
					return
				}
			}
		}
	}
}

func (t *Dep) depositOne(b batch) bool {
	if t.F.Take("FC7") {
		t.setReason("jam", "depositing jam (FC7)", true)
		t.N.Warn("FC7", "depositing stopped by a jam: batter has to wait", b.id)
		go func() {
			if sim.Sleep(t.Ctx, 30*time.Second) {
				t.setReason("jam", "", false)
			}
		}()
	}
	if !t.waitAdmission() || !t.sec.Gate.Wait(t.Ctx) {
		return false
	}
	if t.F.Take("UC5") { // SM1 T1 guard: the Weight-Checker must be calibrated before depositing
		t.N.SetDevice("CheckWeigher", model.StateWarn, "scale out of calibration")
		t.N.Warn("UC5", "Weight-Checker is not calibrated: depositing waits for recalibration", b.id)
		if !sim.Sleep(t.Ctx, 6*time.Second) {
			return false
		}
		t.N.SetDevice("CheckWeigher", model.StateOK, "")
		t.N.Log("UC5", "Weight-Checker recalibrated: depositing resumes", b.id)
	}
	if t.lowStock.Load() { // few clean pans left: fill fewer pans (THM-REQ-14, -15)
		if !sim.Sleep(t.Ctx, 2*time.Second) {
			return false
		}
	}
	// Pan presence check (FC8).
	if t.F.Take("FC8") {
		t.N.SetDevice("PanPresenceEye", model.StateWarn, "no pan at depositing point")
		t.N.Warn("FC8", "no pan at the depositing point: batter held, waiting for pan", b.id)
		t.requestPan()
		if !sim.Sleep(t.Ctx, 4*time.Second) {
			return false
		}
	}
	var panID string
	select {
	case panID = <-t.clean:
		t.dropClean(panID)
	default:
		t.N.SetDevice("PanPresenceEye", model.StateWarn, "waiting for pan")
		select {
		case panID = <-t.clean:
			t.dropClean(panID)
		case <-t.Ctx.Done():
			return false
		}
	}
	t.N.SetDevice("PanPresenceEye", model.StateOK, "")
	if !t.waitAdmission() { // a stop may have come while the pan was awaited
		return false
	}
	if !t.sec.Span(t.Base, depositTime) {
		return false
	}
	fill := b.target + sim.Noise(b.tol*0.3)
	if t.F.Take("FC9") || t.F.Take("FC10") {
		fill = b.target + 3.5*b.tol
	}
	store.Move(t.Ctx, t.Pans, panID, "THM-Depositing", "filled", fmt.Sprintf("%.1f g batter", fill), func(p *model.Pan) {
		p.BatchID, p.FillWeight = b.id, fill
	})
	p := pan{id: panID, batch: b.id}
	if abs(fill-b.target) > b.tol {
		return t.wrongFill(p, fill, b)
	}
	t.mu.Lock()
	t.wrongRun = 0
	t.mu.Unlock()
	t.N.SetDevice("CheckWeigher", model.StateOK, fmt.Sprintf("%.1f g", fill))
	t.N.AddGauge("deposited", 1)
	t.N.SendReliable(sig.New(sig.PanToBaking, "panId", p.id, "batchId", p.batch))
	return true
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

// wrongFill diverts a pan with the wrong fill weight (FC9) and locks the
// depositor out after N_wrong in a row (FC10).
func (t *Dep) wrongFill(p pan, fill float64, b batch) bool {
	t.mu.Lock()
	t.wrongRun++
	run := t.wrongRun
	t.mu.Unlock()
	t.N.SetDevice("CheckWeigher", model.StateWarn, fmt.Sprintf("%.1f g out of range", fill))
	divert(t.Base, p, "FC9", fmt.Sprintf("fill weight %.1f g outside %.0f ± %.0f g: pan diverted, batter discarded", fill, b.target, b.tol))
	if run < nWrong {
		return true
	}
	t.F.Break(depositorMachine)
	t.setReason("lockout", "depositor locked out (FC10)", true)
	t.N.SetDevice("Depositor", model.StateFault, "locked out after repeated wrong fills")
	t.N.Fault("FC10", fmt.Sprintf("wrong fill weight %d times in a row: depositing stopped", run), p.batch, "Depositor", depositorMachine)
	if !t.WaitRepaired(depositorMachine) {
		return false
	}
	t.mu.Lock()
	t.wrongRun = 0
	t.mu.Unlock()
	t.setReason("lockout", "", false)
	t.N.SetDevice("CheckWeigher", model.StateOK, "")
	t.N.Log("FC10", "depositor cleaned and repaired: depositing resumes", p.batch)
	return true
}

func (t *Dep) addClean(id string) {
	t.cmu.Lock()
	dup := t.inClean[id]
	t.inClean[id] = true
	t.cmu.Unlock()
	if !dup {
		t.clean <- id
	}
}

func (t *Dep) dropClean(id string) {
	t.cmu.Lock()
	delete(t.inClean, id)
	t.cmu.Unlock()
}

// restoreSupplied puts back the clean pans that FIN had supplied to depositing
// before this container restarted (the pan documents say which they are).
func (t *Dep) restoreSupplied() {
	all, err := t.Pans.List(t.Ctx, 1000)
	if err != nil {
		return
	}
	n := 0
	for i := len(all) - 1; i >= 0; i-- {
		if p := all[i]; p.Status == "supplied" && p.Station == "FIN-PanSupply" {
			t.addClean(p.ID)
			n++
		}
	}
	if n > 0 {
		log.Printf("[%s] restored %d supplied pans from the stored documents", t.ID, n)
	}
}
