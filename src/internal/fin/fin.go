// Package fin is the Finishing & Packaging system: depanning, quality
// inspection, wrapping and cartoning, palletising and the pan washing loop.
package fin

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"cakefactory/internal/model"
	"cakefactory/internal/sig"
	"cakefactory/internal/sim"
	"cakefactory/internal/store"
	"cakefactory/internal/svc"
)

const (
	depanTime   = 1 * time.Second
	inspectTime = 1 * time.Second
	wrapTime    = 1 * time.Second
	cartonTime  = 1 * time.Second
	washTime    = 600 * time.Millisecond
	dryTime     = 400 * time.Millisecond
	holdingCap  = 6 // holding area in front of wrapping
	maxRejects  = 3 // consecutive rejects before Maintenance is called

	depannerMachine    = "fin-depanner"
	washerMachine      = "fin-washer"
	wrapperMachine     = "fin-wrapper"
	inspectorMachine   = "fin-inspector"
	section4, section5 = 4, 5
)

type cake struct {
	pan, batch string
}

// doc is the id of the cake's own document. After depanning the cake and the
// pan travel separately, so each has its own document.
func (c cake) doc() string { return "CAKE-" + c.batch + "-" + c.pan }

// FIN is the finishing and packaging service.
type FIN struct {
	*svc.Base
	palletSize int
	cooled     chan cake
	toInspect  chan cake
	toWrap     chan cake
	toPallet   chan cake
	washQ      chan string
	panReq     chan struct{}
	gates      [6]*sim.Gate
	slow       [6]atomic.Bool
	halted     atomic.Bool // band halt: finish product inside, accept no new product
	catchup    atomic.Bool // washing station runs at increased speed after a repair

	mu          sync.Mutex
	inWash      map[string]bool // pans queued for washing (drops duplicates)
	inCooled    map[string]bool // cakes queued for depanning (drops duplicates)
	pool        []string
	stopped     map[string]bool // reasons for which pan admission is stopped
	palletNo    int
	rejectRun   int
	rejectCheck string
}

// Run starts the service.
func Run() {
	b, cancel := svc.Run(model.FIN, "Depanner", "VisionPicker", "Checkweigher", "VisionCamera", "MetalDetector",
		"Wrapper", "Cartoner", "CaseCheckweigher", "PalletPacker", "WashingStation", "PanPool", "HoldingArea")
	defer cancel()
	size, _ := strconv.Atoi(svc.Env("PALLET_SIZE", "30"))
	f := &FIN{Base: b, palletSize: size,
		cooled: make(chan cake, 1024), toInspect: make(chan cake, 2), toWrap: make(chan cake, holdingCap),
		toPallet: make(chan cake, 4), washQ: make(chan string, 1024), panReq: make(chan struct{}, 64),
		stopped: map[string]bool{}, inWash: map[string]bool{}, inCooled: map[string]bool{}}
	for i := range f.gates {
		f.gates[i] = sim.NewGate()
	}
	f.N.OnSignal(model.THMCool, sig.CooledCake, func(_ string, s sig.Signal) {
		if s.Float("temp") > 30 { // only cakes released at a safe temperature are accepted (FIN-REQ-02)
			f.N.Warn("UC10", "cake not released at a safe temperature: not accepted", s.Str("batchId"))
			return
		}
		f.enqueueCooled(cake{pan: s.Str("panId"), batch: s.Str("batchId")})
	})
	f.N.OnSignal(model.THMDep, sig.PanRequest, func(string, sig.Signal) { f.panReq <- struct{}{} })
	f.N.OnSignal("", sig.PanForWashing, func(_ string, s sig.Signal) { f.enqueueWash(s.Str("panId")) })
	f.N.OnSignal(model.BT, sig.BandState, func(_ string, s sig.Signal) {
		mode := "run"
		for i := 4; i <= 5; i++ {
			switch s.Str(fmt.Sprintf("S%d", i)) {
			case "halt":
				mode = "halt"
			case "slow":
				if mode != "halt" {
					mode = "slow"
				}
			}
		}
		for i := 4; i <= 5; i++ {
			f.slow[i].Store(mode == "slow")
		}
		if f.halted.Swap(mode == "halt") != (mode == "halt") {
			f.N.Warn("UC2", "band state: "+map[string]string{"run": "running again", "slow": "slow-down: reduced speed", "halt": "halt: finish product inside, accept no new product"}[mode], "")
		}
	})
	f.OnRepair(depannerMachine, func() {
		f.N.SetDevice("Depanner", model.StateOK, "")
		f.N.Log("FC22", "depanning machine repaired: held cakes are processed", "")
	})
	f.OnRepair(washerMachine, func() { // Faulted -> Catch-up: increase speed, log the change (FIN-REQ-19, -20)
		f.N.SetDevice("WashingStation", model.StateOK, "catching up")
		f.catchup.Store(true)
		f.N.Warn("FC23", "washing station repaired: speed increased to work off the backlog", "")
	})
	f.OnRepair(wrapperMachine, func() {
		f.N.SetDevice("Wrapper", model.StateOK, "")
		f.N.Log("FC25", "wrapping station repaired: held cakes are wrapped", "")
	})
	f.OnRepair(inspectorMachine, func() {
		f.N.SetDevice("Checkweigher", model.StateOK, "")
		f.N.Log("FC24", "inspection equipment checked and repaired", "")
	})
	f.seedPans(svc.Env("PAN_COUNT", "60"))
	f.Start()
	for _, fn := range []func(){f.depanner, f.inspector, f.wrapper, f.palletiser, f.washer, f.supplier, f.gaugeLoop} {
		go fn()
	}
	go f.recover()
	<-f.Ctx.Done()
}

// seedPans restores the clean-pan pool from the pan store and creates missing pans.
func (f *FIN) seedPans(count string) {
	n, _ := strconv.Atoi(count)
	all, _ := f.Pans.List(f.Ctx, 500)
	known := map[string]bool{}
	for _, p := range all {
		known[p.ID] = true
		if p.Status == "clean" {
			f.pool = append(f.pool, p.ID)
		}
	}
	for i := 1; i <= n; i++ {
		id := fmt.Sprintf("PAN-%03d", i)
		if !known[id] {
			store.Move(f.Ctx, f.Pans, id, "FIN-PanPool", "clean", "new pan", nil)
			f.pool = append(f.pool, id)
		}
	}
	f.N.Gauge("pool", int64(len(f.pool)))
}

func (f *FIN) span(section int, d time.Duration) bool {
	if !f.gates[section].Wait(f.Ctx) {
		return false
	}
	if f.slow[section].Load() {
		d *= 2
	}
	return sim.Sleep(f.Ctx, d)
}

// admission stops or resumes new pans at THM's depositing for one reason.
func (f *FIN) admission(reason string, stop bool) {
	f.mu.Lock()
	was := len(f.stopped) > 0
	if stop {
		f.stopped[reason] = true
	} else {
		delete(f.stopped, reason)
	}
	now := len(f.stopped) > 0
	f.mu.Unlock()
	if was == now {
		return
	}
	f.N.SetState(sig.New(sig.PanAdmission, "stop", now, "reason", reason))
	if now {
		f.N.Warn("FC25", "asked thermal processing to stop new pans: "+reason, "")
	} else {
		f.N.Log("FC25", "asked thermal processing to resume new pans", "")
	}
}

func (f *FIN) gaugeLoop() {
	t := time.NewTicker(500 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-f.Ctx.Done():
			return
		case <-t.C:
			f.mu.Lock()
			pool := len(f.pool)
			f.mu.Unlock()
			f.N.Gauge("pool", int64(pool))
			f.N.Gauge("depanQ", int64(len(f.cooled)))
			f.N.Gauge("holding", int64(len(f.toWrap)))
			f.N.Gauge("washQ", int64(len(f.washQ)))
			switch {
			case pool == 0:
				f.N.SetDevice("PanPool", model.StateFault, "no clean pans")
			case pool <= 2:
				f.N.SetDevice("PanPool", model.StateWarn, fmt.Sprintf("%d clean pans", pool))
			default:
				f.N.SetDevice("PanPool", model.StateOK, fmt.Sprintf("%d clean pans", pool))
			}
			holding := len(f.toWrap)
			if holding >= holdingCap-1 && f.F.Broken(wrapperMachine) {
				f.admission("wrapping station down, holding area full", true)
			}
			if holding <= 1 && !f.F.Broken(wrapperMachine) {
				f.admission("wrapping station down, holding area full", false)
			}
			if pool <= 1 && f.F.Broken(washerMachine) {
				f.admission("washing station down, clean pans nearly out", true)
			}
			if pool >= 4 && !f.F.Broken(washerMachine) {
				f.admission("washing station down, clean pans nearly out", false)
			}
		}
	}
}

// depanner separates cake and pan (UC11, FC21, FC22).
func (f *FIN) depanner() {
	for {
		for f.halted.Load() { // band halted: accept no new product
			if !sim.Sleep(f.Ctx, 300*time.Millisecond) {
				return
			}
		}
		var c cake
		select {
		case <-f.Ctx.Done():
			return
		case c = <-f.cooled:
			f.mu.Lock()
			delete(f.inCooled, c.batch+"/"+c.pan)
			f.mu.Unlock()
		}
		if f.F.Take("FC22") && !f.F.Broken(depannerMachine) {
			f.F.Break(depannerMachine)
			f.N.SetDevice("Depanner", model.StateFault, "not responding")
			f.N.Fault("FC22", "depanning machine stopped responding: cakes are held, the cake in the machine is removed by hand", c.batch, "Depanner", depannerMachine)
		}
		if !f.WaitRepaired(depannerMachine) || !f.span(section4, depanTime) {
			return
		}
		if f.F.Take("FC21") {
			f.N.SetDevice("VisionPicker", model.StateWarn, "cake not removed correctly")
			store.Move(f.Ctx, f.Pans, c.pan, "FIN-Depanning", "emptied", "cake stuck or broken", nil)
			f.N.Discard("FC21", "cake stuck or broken in its pan: cake rejected, pan sent to washing", c.batch)
			f.N.AddGauge("rejected", 1)
			f.enqueueWash(c.pan)
			continue
		}
		f.N.SetDevice("VisionPicker", model.StateOK, "")
		store.Move(f.Ctx, f.Pans, c.pan, "FIN-Depanning", "depanned", "", nil)
		f.N.AddGauge("depanned", 1)
		f.enqueueWash(c.pan) // the empty pan goes to the washing station
		store.Move(f.Ctx, f.Pans, c.doc(), "FIN-Depanning", "awaiting inspection", "", func(p *model.Pan) { p.PanID, p.BatchID = c.pan, c.batch })
		f.toInspect <- c
	}
}

// inspector checks weight, appearance and metal (UC13, FC24).
func (f *FIN) inspector() {
	var sticky string // while a fault is armed the same check keeps failing
	for {
		var c cake
		select {
		case <-f.Ctx.Done():
			return
		case c = <-f.toInspect:
		}
		if !f.WaitRepaired(inspectorMachine) || !f.span(section4, inspectTime) {
			return
		}
		reason := ""
		if f.F.Take("FC24") {
			if sticky == "" {
				sticky = [...]string{"wrong weight", "visual defect", "metal contamination"}[time.Now().UnixNano()%3]
			}
			reason = sticky
			if !f.F.Armed("FC24") {
				sticky = ""
			}
		}
		if reason == "" {
			f.mu.Lock()
			f.rejectRun, f.rejectCheck = 0, ""
			f.mu.Unlock()
			store.Move(f.Ctx, f.Pans, c.doc(), "FIN-Inspection", "inspected", "passed weight, visual and metal checks", nil)
			f.N.AddGauge("inspected", 1)
			f.pushToWrap(c)
			continue
		}
		store.Move(f.Ctx, f.Pans, c.doc(), "FIN-Inspection", "rejected", reason, nil)
		f.N.AddGauge("rejected", 1)
		f.N.Discard("FC24", "cake rejected at quality inspection: "+reason, c.batch)
		if reason == "metal contamination" {
			f.N.Warn("FC24", "metal found: Food Quality Control Department notified", c.batch)
		}
		f.mu.Lock()
		if reason == f.rejectCheck {
			f.rejectRun++
		} else {
			f.rejectRun, f.rejectCheck = 1, reason
		}
		run := f.rejectRun
		f.mu.Unlock()
		if run >= maxRejects {
			f.F.Break(inspectorMachine)
			f.N.SetDevice("Checkweigher", model.StateFault, "same check failed repeatedly")
			f.N.Fault("FC24", fmt.Sprintf("%q failed %d times in a row: the machine is probably at fault", reason, run), c.batch, "Checkweigher", inspectorMachine)
			f.mu.Lock()
			f.rejectRun = 0
			f.mu.Unlock()
		}
	}
}

// pushToWrap puts a cake into the holding area in front of the wrapper.
func (f *FIN) pushToWrap(c cake) {
	select {
	case f.toWrap <- c:
	default:
		f.N.SetDevice("HoldingArea", model.StateWarn, "full")
		f.admission("wrapping station down, holding area full", true)
		select {
		case f.toWrap <- c:
		case <-f.Ctx.Done():
		}
		f.N.SetDevice("HoldingArea", model.StateOK, "")
	}
}

// wrapper wraps cakes and checks them (UC14, FC25).
func (f *FIN) wrapper() {
	for {
		var c cake
		select {
		case <-f.Ctx.Done():
			return
		case c = <-f.toWrap:
		}
		if f.F.Take("FC25") && !f.F.Broken(wrapperMachine) {
			f.F.Break(wrapperMachine)
			f.N.SetDevice("Wrapper", model.StateFault, "wrapping station broken")
			f.N.Fault("FC25", "wrapping station broke: incoming cakes are held, new pans stop when the holding area is full", c.batch, "Wrapper", wrapperMachine)
		}
		if !f.WaitRepaired(wrapperMachine) || !f.span(section5, wrapTime) {
			return
		}
		if f.F.Take("UC14") { // wrap or carton check fails: refuse bin, only properly wrapped cakes ship
			store.Move(f.Ctx, f.Pans, c.doc(), "FIN-Wrapping", "refused", "wrap or carton defective", nil)
			f.N.Discard("UC14", "wrapped or cartoned cake defective: moved to refuse bin", c.batch)
			f.N.AddGauge("rejected", 1)
			continue
		}
		store.Move(f.Ctx, f.Pans, c.doc(), "FIN-Wrapping", "wrapped", "", nil)
		f.toPallet <- c // cartoning and case check happen in the palletiser
	}
}

// palletiser cartons, checks case weight and stacks cakes on pallets (UC16).
func (f *FIN) palletiser() {
	var batches []string
	count := 0
	for {
		var c cake
		select {
		case <-f.Ctx.Done():
			return
		case c = <-f.toPallet:
		}
		if !f.span(section5, cartonTime) {
			return
		}
		if f.F.Take("FC26") {
			f.N.SetDevice("CaseCheckweigher", model.StateWarn, "wrong case weight")
			store.Move(f.Ctx, f.Pans, c.doc(), "FIN-Cartoning", "set aside", "wrong case weight", nil)
			f.N.Warn("FC26", "wrong case weight: case set aside for manual check", c.batch)
			continue
		}
		f.N.SetDevice("CaseCheckweigher", model.StateOK, "")
		store.Move(f.Ctx, f.Pans, c.doc(), "FIN-Pallet", "packed", "", nil)
		f.N.AddGauge("packed", 1)
		count++
		if !strings.Contains(strings.Join(batches, ","), c.batch) {
			batches = append(batches, c.batch)
		}
		f.N.Gauge("palletFill", int64(count))
		if count < f.palletSize {
			continue
		}
		f.span(section5, wrapTime) // wrap the pallet
		f.mu.Lock()
		f.palletNo++
		id := fmt.Sprintf("PAL-%04d", f.palletNo)
		f.mu.Unlock()
		f.N.AddGauge("pallets", 1)
		f.N.SendReliable(sig.New(sig.PalletReady, "palletId", id, "cakes", int64(count), "batches", strings.Join(batches, ",")))
		f.N.Log("UC16", fmt.Sprintf("pallet %s full (%d cakes), wrapped and ready for shipment", id, count), strings.Join(batches, ","))
		count, batches = 0, nil
		f.N.Gauge("palletFill", 0)
	}
}

// washer washes, dries and verifies empty pans (UC12, FC23).
func (f *FIN) washer() {
	for {
		var id string
		select {
		case <-f.Ctx.Done():
			return
		case id = <-f.washQ:
		}
		if f.F.Take("FC23") && !f.F.Broken(washerMachine) {
			f.F.Break(washerMachine)
			f.N.SetDevice("WashingStation", model.StateFault, "washing station broken")
			f.N.Fault("FC23", "washing station broke: depositing uses the clean pans in stock", "", "WashingStation", washerMachine)
		}
		if !f.WaitRepaired(washerMachine) {
			return
		}
		d := washTime + dryTime
		if f.catchup.Load() {
			d /= 2
		}
		if !sim.Sleep(f.Ctx, d) {
			return
		}
		if f.catchup.Load() && len(f.washQ) == 0 { // backlog = 0: normal speed, log the change
			f.catchup.Store(false)
			f.N.SetDevice("WashingStation", model.StateOK, "")
			f.N.Log("UC12", "backlog cleared: washing speed back to normal", "")
		}
		if f.F.Take("UC12") {
			store.Move(f.Ctx, f.Pans, id, "FIN-Washing", "discarded", "damaged pan", nil)
			f.N.Discard("UC12", "washed pan "+id+" is damaged: sorted out of the production cycle", "")
			f.N.AddGauge("pansDiscarded", 1)
			f.mu.Lock()
			delete(f.inWash, id)
			f.mu.Unlock()
			continue
		}
		store.Move(f.Ctx, f.Pans, id, "FIN-PanPool", "clean", "washed and dried", func(p *model.Pan) { p.BatchID, p.FillWeight, p.CakeTemp = "", 0, 0 })
		f.N.AddGauge("washed", 1)
		f.mu.Lock()
		f.pool = append(f.pool, id)
		delete(f.inWash, id)
		f.mu.Unlock()
	}
}

// supplier answers THM's pan requests from the clean pool.
func (f *FIN) supplier() {
	for {
		select {
		case <-f.Ctx.Done():
			return
		case <-f.panReq:
		}
		f.mu.Lock()
		id, stock := "", len(f.pool)
		if stock > 0 {
			id, f.pool = f.pool[0], f.pool[1:]
			stock--
		}
		f.mu.Unlock()
		if id != "" {
			store.Move(f.Ctx, f.Pans, id, "FIN-PanSupply", "supplied", "sent to depositing", nil)
		}
		f.N.SendReliable(sig.New(sig.CleanPanSupply, "panId", id, "stock", int64(stock)))
	}
}

func (f *FIN) enqueueWash(id string) {
	if id == "" {
		return
	}
	f.mu.Lock()
	dup := f.inWash[id]
	f.inWash[id] = true
	f.mu.Unlock()
	if !dup {
		f.washQ <- id
	}
}

func (f *FIN) enqueueCooled(c cake) {
	key := c.batch + "/" + c.pan
	f.mu.Lock()
	dup := f.inCooled[key]
	f.inCooled[key] = true
	f.mu.Unlock()
	if !dup {
		f.cooled <- c
	}
}

// recover rebuilds the in-memory queues from the stored documents after a
// restart: the pan and cake documents say where every one of them was. Messages
// that arrived while FIN was down are queued by the broker and deduplicated
// against what is rebuilt here.
func (f *FIN) recover() {
	all, err := f.Pans.List(f.Ctx, 1000)
	if err != nil || len(all) == 0 {
		return
	}
	n := 0
	for i := len(all) - 1; i >= 0; i-- { // oldest first
		p := all[i]
		switch {
		case strings.HasPrefix(p.ID, "CAKE-"):
			c := cake{pan: p.PanID, batch: p.BatchID}
			var q chan cake
			switch p.Status {
			case "awaiting inspection":
				q = f.toInspect
			case "inspected":
				q = f.toWrap
			case "wrapped":
				q = f.toPallet
			}
			if q != nil {
				q <- c
				n++
			}
		case p.Status == "cooled" && p.Station == "THM-Cooling":
			f.enqueueCooled(cake{pan: p.ID, batch: p.BatchID})
			n++
		case p.Status == "depanned" || p.Status == "emptied" || (p.Status == "discarded" && p.Station == "THM"):
			f.enqueueWash(p.ID)
			n++
		}
	}
	if n > 0 {
		f.N.Log("FC27", fmt.Sprintf("restart: %d cakes and pans put back in their queues from the stored documents", n), "")
	}
}
