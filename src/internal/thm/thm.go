// Package thm is the Thermal Processing system: depositing, baking zones,
// tunnel-exit inspection and cooling with its fallback buffer.
package thm

import (
	"fmt"
	"log"
	"math"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"cakefactory/internal/model"
	"cakefactory/internal/node"
	"cakefactory/internal/sig"
	"cakefactory/internal/sim"
	"cakefactory/internal/store"
	"cakefactory/internal/svc"
)

// Parameters (simulated time and degrees Celsius).
const (
	nWrong         = 3               // N_wrong: fills out of tolerance in a row
	tAdjust        = 4 * time.Second // T_adjust
	depositTime    = 1 * time.Second
	zoneTransit    = 2 * time.Second
	exitCheckTime  = 500 * time.Millisecond
	coolingTime    = 4 * time.Second
	maxBufferWait  = 40 * time.Second // stands for the 30 minutes of UC10
	bufferCapacity = 8
	tSafe          = 30.0
	bakeTarget     = 97.0
	bakeTol        = 0.5

	depositorMachine = "thm-depositor"
	zoneMachine      = "thm-zone-2"
	cameraMachine    = "thm-heatcamera"
	coolingMachine   = "thm-cooling"
)

var zoneSetpoint = [4]float64{0, 160, 180, 170}

type batch struct {
	id     string
	pans   int
	target float64
	tol    float64
}

type pan struct {
	id, batch string
	off       float64 // temperature offset picked up in a faulty zone
}

type bufItem struct {
	pan   pan
	temp  float64
	since time.Time
}

// THM is the thermal processing service.
type THM struct {
	*svc.Base
	batters chan batch
	clean   chan string
	pending atomic.Int64 // pan requests not answered yet
	gates   [6]*sim.Gate // band sections S1..S3 are ours
	slow    [6]atomic.Bool

	cmu       sync.Mutex
	inClean   map[string]bool // clean pans waiting at depositing (drops duplicates)
	seenBatch map[string]bool
	mu        sync.Mutex
	reasons   map[string]string // reasons why new pans are not admitted
	notified  bool
	buf       []bufItem
	wrongRun  int
	zoneOff   [4]float64
	zoneSince time.Time
	zoneFault bool

	inTunnel, inCooling atomic.Int64
	lastLowStock        time.Time
	lowStock            atomic.Bool
	coolFailAt          time.Time
	expireAll           bool
}

// Run starts the service.
func Run() {
	b, cancel := svc.Run(model.THM, "Depositor", "PanPresenceEye", "CheckWeigher", "Zone1", "Zone2", "Zone3",
		"HeatCamera", "CoolingControl", "CoolingBuffer")
	defer cancel()
	t := &THM{Base: b, batters: make(chan batch, 1024), clean: make(chan string, 1024), reasons: map[string]string{}, inClean: map[string]bool{}, seenBatch: map[string]bool{}}
	for i := range t.gates {
		t.gates[i] = sim.NewGate()
	}
	t.N.OnSignal(model.BAT, sig.BatterReady, func(_ string, s sig.Signal) {
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
	t.N.OnSignal(model.FIN, sig.CleanPanSupply, func(_ string, s sig.Signal) {
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
	t.N.OnSignal(model.FIN, sig.PanAdmission, func(_ string, s sig.Signal) {
		t.setReason("fin", s.Str("reason"), s.Bool("stop"))
	})
	t.N.OnSignal(model.BT, sig.BandState, func(_ string, s sig.Signal) { t.onBand(s) })
	t.OnRepair(depositorMachine, func() {})
	t.OnRepair(zoneMachine, t.zoneRepaired)
	t.OnRepair(cameraMachine, func() {
		t.N.SetDevice("HeatCamera", model.StateOK, "")
		t.N.Log("FC13", "heat camera repaired: normal checking resumes", "")
	})
	t.OnRepair(coolingMachine, t.coolingRepaired)
	// The report requires that production may not run without the monitor.
	t.setReason("monitor", "monitor offline: production may not run without it", true)
	t.N.OnHost(func(on bool) {
		t.setReason("monitor", "monitor offline: production may not run without it", !on)
		if !on {
			t.N.Warn("UC2", "monitor went offline: no new pans are admitted", "")
		}
	})
	t.restoreSupplied()
	t.restoreBuffered()
	t.Start()
	go t.resumeInFlight()
	go t.zoneLoop()
	go t.releaseLoop()
	go t.panRefill()
	go t.gaugeLoop()
	t.depositor()
}

// onBand applies "Band state change" for the three sections THM owns. Halt:
// accept no new pans, product inside finishes (THM-REQ-65). Slow: reduced speed
// (THM-REQ-66). Restart: resume if no other stop reason is active (THM-REQ-25, -67).
func (t *THM) onBand(s sig.Signal) {
	mode := "run"
	for i := 1; i <= 3; i++ {
		switch s.Str(fmt.Sprintf("S%d", i)) {
		case "halt":
			mode = "halt"
		case "slow":
			if mode != "halt" {
				mode = "slow"
			}
		}
	}
	for i := 1; i <= 3; i++ {
		t.slow[i].Store(mode == "slow")
	}
	t.gates[1].Set(s.Str("S1") != "halt") // the depositor works on section 1: a halted S1 stops it at once
	t.setReason("band", "band halted: no new pans", mode == "halt")
}

// span waits for the band section to run and then simulates d of work.
func (t *THM) span(section int, d time.Duration) bool {
	if !t.gates[section].Wait(t.Ctx) {
		return false
	}
	if t.slow[section].Load() {
		d *= 2
	}
	return sim.Sleep(t.Ctx, d)
}

// setReason stops or resumes pan admission and tells batter preparation.
func (t *THM) setReason(key, reason string, on bool) {
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

func (t *THM) admitting() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.reasons) == 0
}

func (t *THM) waitAdmission() bool {
	for !t.admitting() {
		if !sim.Sleep(t.Ctx, 300*time.Millisecond) {
			return false
		}
	}
	return true
}

func (t *THM) requestPan() {
	t.pending.Add(1)
	t.N.SendReliable(sig.New(sig.PanRequest, "message", "waiting for pan"))
}

func (t *THM) panRefill() {
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

func (t *THM) gaugeLoop() {
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-t.Ctx.Done():
			return
		case <-tick.C:
			t.N.Gauge("queue", int64(len(t.batters)))
			t.N.Gauge("tunnel", t.inTunnel.Load())
			t.N.Gauge("cooling", t.inCooling.Load())
			t.mu.Lock()
			t.N.Gauge("buffer", int64(len(t.buf)))
			t.mu.Unlock()
		}
	}
}

func (t *THM) depositor() {
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

func (t *THM) depositOne(b batch) bool {
	if t.F.Take("FC7") {
		t.setReason("jam", "depositing jam (FC7)", true)
		t.N.Warn("FC7", "depositing stopped by a jam: batter has to wait", b.id)
		go func() {
			if sim.Sleep(t.Ctx, 30*time.Second) {
				t.setReason("jam", "", false)
			}
		}()
	}
	if !t.waitAdmission() || !t.gates[1].Wait(t.Ctx) {
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
	if !t.span(1, depositTime) {
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
	if math.Abs(fill-b.target) > b.tol {
		return t.wrongFill(p, fill, b)
	}
	t.mu.Lock()
	t.wrongRun = 0
	t.mu.Unlock()
	t.N.SetDevice("CheckWeigher", model.StateOK, fmt.Sprintf("%.1f g", fill))
	t.N.AddGauge("deposited", 1)
	go t.bake(p)
	return true
}

// wrongFill diverts a pan with the wrong fill weight (FC9) and locks the
// depositor out after N_wrong in a row (FC10).
func (t *THM) wrongFill(p pan, fill float64, b batch) bool {
	t.mu.Lock()
	t.wrongRun++
	run := t.wrongRun
	t.mu.Unlock()
	t.N.SetDevice("CheckWeigher", model.StateWarn, fmt.Sprintf("%.1f g out of range", fill))
	t.divert(p, "FC9", fmt.Sprintf("fill weight %.1f g outside %.0f ± %.0f g: pan diverted, batter discarded", fill, b.target, b.tol))
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

// divert sends an emptied pan to washing and records a discard.
func (t *THM) divert(p pan, code, reason string) {
	store.Move(t.Ctx, t.Pans, p.id, "THM", "discarded", reason, nil)
	t.N.Discard(code, reason, p.batch)
	t.N.AddGauge("discarded", 1)
	t.N.SendReliable(sig.New(sig.PanForWashing, "panId", p.id, "batchId", p.batch, "reason", code))
}

// bake moves a pan through the three zones and to the tunnel exit.
func (t *THM) bake(p pan) {
	t.inTunnel.Add(1)
	store.Move(t.Ctx, t.Pans, p.id, "THM-Baking", "baking", "", nil)
	for z := 1; z <= 3; z++ {
		if !t.span(2, zoneTransit) {
			return
		}
		t.mu.Lock()
		p.off += t.zoneOff[z]
		t.mu.Unlock()
	}
	t.inTunnel.Add(-1)
	t.exitCheck(p)
}

func (t *THM) exitCheck(p pan) {
	if !t.span(2, exitCheckTime) {
		return
	}
	if t.F.Take("FC13") && !t.F.Broken(cameraMachine) {
		t.F.Break(cameraMachine)
		t.N.SetDevice("HeatCamera", model.StateFault, "not responding")
		t.N.Fault("FC13", "heat camera stopped responding: all cakes are diverted until it is repaired", p.batch, "HeatCamera", cameraMachine)
	}
	if t.F.Broken(cameraMachine) {
		t.divert(p, "FC13", "heat camera not responding: cake cannot be checked, diverted to refuse")
		return
	}
	t.mu.Lock()
	full := len(t.buf) >= bufferCapacity
	t.mu.Unlock()
	if full { // buffer full: the exit diverts all cakes until it has free places (THM-REQ-58, -60)
		t.setReason("buffer", "cooling buffer full (FC18)", true)
		t.divert(p, "FC18", "cooling buffer full: tunnel exit diverts all cakes")
		return
	}
	temp := bakeTarget + sim.Noise(0.15) + p.off*0.2
	if t.F.Take("FC12") {
		temp = 99.4
		if sim.Chance(0.5) {
			temp = 94.1
		}
	}
	store.Move(t.Ctx, t.Pans, p.id, "THM-TunnelExit", "baked", fmt.Sprintf("core %.2f C", temp), func(m *model.Pan) { m.CakeTemp = temp })
	if math.Abs(temp-bakeTarget) > bakeTol {
		kind := "overcooked"
		if temp < bakeTarget {
			kind = "undercooked"
		}
		t.divert(p, "FC12", fmt.Sprintf("cake %s: %.2f C, allowed %.1f ± %.1f C", kind, temp, bakeTarget, bakeTol))
		return
	}
	t.N.AddGauge("baked", 1)
	t.cool(p, temp)
}

// cool sends the cake through the cooling zone or, if cooling is out of
// service or the cake is still too hot, into the cooling buffer.
func (t *THM) cool(p pan, temp float64) {
	if !t.F.Broken(coolingMachine) && t.F.Take("FC15") {
		t.F.Break(coolingMachine)
		t.mu.Lock()
		t.coolFailAt = time.Now()
		t.mu.Unlock()
		t.N.SetDevice("CoolingControl", model.StateFault, "cooling stopped working")
		t.N.Fault("FC15", "cooling stopped working: diverter raised, new cakes go to the cooling buffer", p.batch, "CoolingControl", coolingMachine)
	}
	if t.F.Broken(coolingMachine) {
		t.buffer(p, temp, "cooling out of service")
		return
	}
	t.inCooling.Add(1)
	store.Move(t.Ctx, t.Pans, p.id, "THM-Cooling", "cooling", "", nil)
	ok := t.span(3, coolingTime)
	t.inCooling.Add(-1)
	if !ok {
		return
	}
	temp = 24 + sim.Noise(1.2)
	if t.F.Take("FC14") {
		temp = 38
	}
	switch {
	case t.F.Broken(coolingMachine):
		t.buffer(p, temp, "cooling stopped while the cake was inside")
	case temp > tSafe:
		t.N.Warn("FC14", fmt.Sprintf("cake still too hot after cooling (%.1f C): cooling increased, cake held in buffer", temp), p.batch)
		t.buffer(p, 60, "still too hot")
	default:
		t.deliver(p, temp)
	}
}

func (t *THM) deliver(p pan, temp float64) {
	store.Move(t.Ctx, t.Pans, p.id, "THM-Cooling", "cooled", fmt.Sprintf("core %.1f C", temp), func(m *model.Pan) { m.CakeTemp = temp })
	t.N.AddGauge("cooled", 1)
	t.N.SendReliable(sig.New(sig.CooledCake, "panId", p.id, "batchId", p.batch, "temp", temp))
}

// buffer holds a cake in the separate-level cooling buffer (FC15, FC14).
func (t *THM) buffer(p pan, temp float64, why string) {
	t.mu.Lock()
	full := len(t.buf) >= bufferCapacity
	if !full {
		t.buf = append(t.buf, bufItem{pan: p, temp: temp, since: time.Now()})
	}
	n := len(t.buf)
	t.mu.Unlock()
	if full {
		t.N.Emit(node.Event{Severity: model.SevError, Code: "FC18", BatchID: p.batch,
			Reason: "cooling buffer full: new pans stopped, cakes leaving the tunnel go to refuse"})
		t.setReason("buffer", "cooling buffer full (FC18)", true)
		t.divert(p, "FC18", "cooling buffer full: cake diverted to refuse")
		return
	}
	store.Move(t.Ctx, t.Pans, p.id, "THM-CoolingBuffer", "buffered", why, nil)
	t.N.SetDevice("CoolingBuffer", model.StateWarn, fmt.Sprintf("%d cakes held", n))
	t.N.Log("FC15", fmt.Sprintf("cake held in cooling buffer (%s), %d/%d", why, n, bufferCapacity), p.batch)
	if n >= bufferCapacity { // FC18: report "buffer full" at once, stop new pans, the exit diverts what still comes
		t.setReason("buffer", "cooling buffer full (FC18)", true)
		t.N.SetDevice("CoolingBuffer", model.StateFault, "full")
		t.N.Emit(node.Event{Severity: model.SevError, Code: "FC18", BatchID: p.batch,
			Reason: fmt.Sprintf("cooling buffer full (%d/%d): new pans stopped, cakes leaving the tunnel are diverted to refuse", n, bufferCapacity)})
	}
}

func (t *THM) coolingRepaired() {
	t.N.SetDevice("CoolingControl", model.StateOK, "")
	t.mu.Lock()
	down := time.Duration(0) // after a restart the failure time is unknown: assume in time
	if !t.coolFailAt.IsZero() {
		down = sim.Unscale(time.Since(t.coolFailAt))
	}
	t.expireAll = down > maxBufferWait
	n := len(t.buf)
	t.mu.Unlock()
	if down > maxBufferWait {
		t.N.Log("FC16", "cooling repaired after more than 30 minutes: buffered cakes go to refuse", "")
	} else {
		t.N.Log("FC16", "cooling repaired in time: diverter lowered, buffered cakes are released in arrival order and measured", "")
	}
	if n < bufferCapacity {
		t.setReason("buffer", "", false)
	}
}

// releaseLoop releases buffered cakes once cooling works (FC16) and discards
// cakes that waited too long (FC17).
func (t *THM) releaseLoop() {
	tick := time.NewTicker(sim.Scale(700 * time.Millisecond))
	defer tick.Stop()
	for {
		select {
		case <-t.Ctx.Done():
			return
		case <-tick.C:
		}
		var expired, ready []bufItem
		t.mu.Lock()
		keep := t.buf[:0]
		for _, it := range t.buf {
			if t.expireAll || time.Since(it.since) > sim.Scale(maxBufferWait) {
				expired = append(expired, it)
			} else {
				keep = append(keep, it)
			}
		}
		t.buf = keep
		t.expireAll = false
		if len(t.buf) > 0 && !t.F.Broken(coolingMachine) {
			ready = append(ready, t.buf[0])
			t.buf = t.buf[1:]
		}
		n := len(t.buf)
		t.mu.Unlock()
		for _, it := range expired {
			t.divert(it.pan, "FC17", fmt.Sprintf("cake waited %.0f s in the cooling buffer (limit %.0f s): discarded", sim.Unscale(time.Since(it.since)).Seconds(), maxBufferWait.Seconds()))
		}
		for _, it := range ready { // released cake is measured: cool again or send on
			temp := 24 + (it.temp-24)*math.Exp(-sim.Unscale(time.Since(it.since)).Seconds()/20)
			if temp > tSafe {
				go t.cool(it.pan, temp)
			} else {
				t.deliver(it.pan, temp)
			}
		}
		if len(expired)+len(ready) > 0 {
			if n == 0 {
				t.N.SetDevice("CoolingBuffer", model.StateOK, "")
			} else {
				t.N.SetDevice("CoolingBuffer", model.StateWarn, fmt.Sprintf("%d cakes held", n))
			}
			if n < bufferCapacity && !t.F.Broken(coolingMachine) {
				t.setReason("buffer", "", false)
			}
		}
	}
}

// zoneLoop simulates the zone temperature control loops (FC11).
func (t *THM) zoneLoop() {
	tick := time.NewTicker(sim.Scale(time.Second))
	defer tick.Stop()
	for {
		select {
		case <-t.Ctx.Done():
			return
		case <-tick.C:
		}
		t.mu.Lock()
		if t.F.Take("FC11") && t.zoneOff[2] == 0 {
			t.zoneOff[2] = 28
			t.zoneSince = time.Now()
			t.mu.Unlock()
			t.N.SetDevice("Zone2", model.StateWarn, "temperature out of range")
			t.N.Warn("FC11", "baking zone 2 temperature out of range (+28 C): adjusting zone heating", "")
			t.mu.Lock()
		}
		faultNow := t.zoneOff[2] != 0 && !t.zoneFault && time.Since(t.zoneSince) > sim.Scale(tAdjust)
		if faultNow {
			t.zoneFault = true
		}
		var temps [4]float64
		for z := 1; z <= 3; z++ {
			temps[z] = zoneSetpoint[z] + t.zoneOff[z] + sim.Noise(0.6)
		}
		t.mu.Unlock()
		for z := 1; z <= 3; z++ {
			t.N.Gauge(fmt.Sprintf("zone%d", z), int64(math.Round(temps[z])))
		}
		if faultNow {
			t.N.SetDevice("Zone2", model.StateFault, "still out of range after adjustment")
			t.setReason("zone", "baking zone 2 faulty (FC11)", true)
			t.N.Fault("FC11", "zone 2 still out of range after adjustment: flagged faulty, new pans stopped; pans inside finish and are checked at the exit",
				"", "Zone2", zoneMachine)
		}
	}
}

func (t *THM) zoneRepaired() {
	t.mu.Lock()
	t.zoneOff[2] = 0
	t.zoneFault = false
	t.mu.Unlock()
	t.N.SetDevice("Zone2", model.StateOK, "")
	t.setReason("zone", "", false)
	t.N.Log("FC11", "zone 2 repaired: depositing resumes", "")
}

func (t *THM) addClean(id string) {
	t.cmu.Lock()
	dup := t.inClean[id]
	t.inClean[id] = true
	t.cmu.Unlock()
	if !dup {
		t.clean <- id
	}
}

func (t *THM) dropClean(id string) {
	t.cmu.Lock()
	delete(t.inClean, id)
	t.cmu.Unlock()
}

// restoreSupplied puts back the clean pans that FIN had supplied to depositing
// before this container restarted (the pan documents say which they are).
func (t *THM) restoreSupplied() {
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
		log.Printf("[THM] restored %d supplied pans from the stored documents", n)
	}
}

// restoreBuffered puts the cakes of the cooling buffer back. The buffer lives on
// a separate level, so the cakes are still there after a restart, and their
// waiting time continues from the time they were put in (THM deployment table:
// "cooling-buffer waiting times are kept in saved state").
func (t *THM) restoreBuffered() {
	all, err := t.Pans.List(t.Ctx, 1000)
	if err != nil {
		return
	}
	for i := len(all) - 1; i >= 0; i-- {
		if p := all[i]; p.Status == "buffered" && p.Station == "THM-CoolingBuffer" {
			t.buf = append(t.buf, bufItem{pan: pan{id: p.ID, batch: p.BatchID}, temp: 60, since: p.Updated})
		}
	}
	if len(t.buf) > 0 {
		log.Printf("[THM] restored %d cakes of the cooling buffer", len(t.buf))
		t.N.SetDevice("CoolingBuffer", model.StateWarn, fmt.Sprintf("%d cakes held", len(t.buf)))
	}
}

// resumeInFlight takes up the cakes that were inside the tunnel and the cooling
// zone when this container restarted. Nothing is in memory at that point, so
// every pan document that says "filled", "baking", "baked" or "cooling" is a cake
// that physically sits on the band and must not be forgotten (its pan would be
// lost from the pan loop, against the double-pans rule). The heaters kept their
// last command, so the cakes simply continue from the stage they were at.
func (t *THM) resumeInFlight() {
	all, err := t.Pans.List(t.Ctx, 1000)
	if err != nil {
		return
	}
	n := 0
	for i := len(all) - 1; i >= 0; i-- {
		p := all[i]
		c := pan{id: p.ID, batch: p.BatchID}
		switch {
		case p.Status == "filled" && p.Station == "THM-Depositing":
			if math.Abs(p.FillWeight-store.SeedRecipe.FillTarget) > store.SeedRecipe.FillTol {
				t.divert(c, "FC9", fmt.Sprintf("restart: fill weight %.1f g was out of tolerance: pan diverted", p.FillWeight))
				continue
			}
			go t.bake(c)
		case p.Status == "baking" && p.Station == "THM-Baking":
			go t.bake(c) // the pan continues through the zones
		case p.Status == "baked" && p.Station == "THM-TunnelExit":
			go t.exitCheck(c)
		case p.Status == "cooling" && p.Station == "THM-Cooling":
			go t.cool(c, p.CakeTemp)
		default:
			continue
		}
		n++
	}
	if n > 0 {
		t.N.Log("FC27", fmt.Sprintf("restart: %d cakes on the band taken up again from the stored documents", n), "")
	}
}
