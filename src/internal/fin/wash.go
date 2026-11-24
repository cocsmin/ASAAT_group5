package fin

import (
	"fmt"
	"log"
	"strconv"
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

const washReason = "washing station down, clean pans nearly out"

// A damaged pan (UC12) is replaced by a new one after this time (simulated). It is a variable so that
// the test does not have to wait.
var replaceDelay = 30 * time.Second

// Wash is the pan washing station. It also keeps the clean-pan pool and answers the
// pan requests of depositing from it (UC12, FC23).
type Wash struct {
	*svc.Base
	adm     *station.Admission
	took    *station.PanSet // pans queued for washing (drops duplicates)
	washQ   chan string
	panReq  chan struct{}
	catchup atomic.Bool // washing runs at increased speed after a repair

	mu      sync.Mutex
	pool    []string // the pan hopper: clean pans, the reserve included
	nextPan int      // number of the next new pan
}

// RunWash starts the pan washing station.
func RunWash() {
	b, cancel := svc.Run(model.FINWash, "WashingStation", "PanPool")
	defer cancel()
	f := &Wash{Base: b, took: station.NewPanSet(), washQ: make(chan string, 4096), panReq: make(chan struct{}, 256)}
	f.adm = &station.Admission{B: b}
	f.N.OnSignal(model.THMDep, sig.PanRequest, func(string, sig.Signal) { f.panReq <- struct{}{} })
	f.N.OnSignal("", sig.PanForWashing, func(_ string, s sig.Signal) { f.enqueue(s.Str("panId")) })
	f.OnRepair(washerMachine, func() { // Faulted -> Catch-up: increase speed, log the change (FIN-REQ-19, -20)
		f.N.SetDevice("WashingStation", model.StateOK, "catching up")
		f.catchup.Store(true)
		f.N.Warn("FC23", "washing station repaired: speed increased to work off the backlog", "")
	})
	reserve, _ := strconv.Atoi(svc.Env("PAN_RESERVE_PERCENT", "100"))
	f.seedPans(svc.Env("PAN_COUNT", "48"), reserve)
	f.Start()
	f.adm.Reset()
	go f.gaugeLoop()
	go f.supplier()
	station.Reconcile(f.Base, f.adopt)
	f.washer()
}

// seedPans restores the pan hopper from the pan store and creates the missing pans. The loop has
// count pans; the hopper keeps a reserve of reservePercent of them on top (100: as many again), so
// that depositing does not run out of clean pans while the washing station is down or a damaged pan
// is not replaced yet. A pan that was sorted out (UC12) does not count: a new one takes its place.
func (f *Wash) seedPans(count string, reservePercent int) {
	n, _ := strconv.Atoi(count)
	total := n + n*reservePercent/100
	all, _ := f.Pans.List(f.Ctx, 1000)
	active, maxNo := 0, 0
	for _, p := range all {
		if p.Status == "clean" {
			f.pool = append(f.pool, p.ID)
		}
		var no int
		if _, err := fmt.Sscanf(p.ID, "PAN-%d", &no); err == nil && len(p.ID) > 4 && p.ID[:4] == "PAN-" {
			if no > maxNo {
				maxNo = no
			}
			if !(p.Status == "discarded" && p.Station == "FIN-Washing") { // a pan diverted at THM is still on its way to washing
				active++
			}
		}
	}
	f.nextPan = maxNo + 1
	for ; active < total; active++ {
		id := f.newPanID()
		store.Move(f.Ctx, f.Pans, id, "FIN-PanPool", "clean", "new pan", nil)
		f.pool = append(f.pool, id)
	}
	f.N.Gauge("pool", int64(len(f.pool)))
}

// newPanID gives the number of a new pan.
func (f *Wash) newPanID() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := fmt.Sprintf("PAN-%03d", f.nextPan)
	f.nextPan++
	return id
}

// replace puts a new pan into the hopper, some time after a damaged one was sorted out.
func (f *Wash) replace() {
	if !sim.Sleep(f.Ctx, replaceDelay) {
		return
	}
	id := f.newPanID()
	store.Move(f.Ctx, f.Pans, id, "FIN-PanPool", "clean", "replacement pan", nil)
	f.mu.Lock()
	f.pool = append(f.pool, id)
	f.mu.Unlock()
	f.N.AddGauge("pansReplaced", 1)
	f.N.Log("UC12", "replacement pan "+id+" put into the pan hopper", "")
}

func (f *Wash) enqueue(id string) {
	if id != "" && f.took.Add(id) {
		f.washQ <- id
	}
}

// adopt takes up the empty pans that depanning (or a diverting station) handed over
// without a message arriving, and the ones that were waiting here at a restart.
func (f *Wash) adopt() {
	all, err := f.Pans.List(f.Ctx, 1000)
	if err != nil {
		return
	}
	n := 0
	for i := len(all) - 1; i >= 0; i-- {
		p := all[i]
		handed := (p.Station == "FIN-Depanning" && (p.Status == "depanned" || p.Status == "emptied")) || (p.Station == "THM" && p.Status == "discarded")
		if handed && time.Since(p.Updated) > station.HandoverGrace && f.took.Add(p.ID) {
			f.washQ <- p.ID
			n++
		}
	}
	if n > 0 {
		log.Printf("[%s] %d empty pans taken up from the stored documents", f.ID, n)
	}
}

func (f *Wash) gaugeLoop() {
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
			f.N.Gauge("washQ", int64(len(f.washQ)))
			switch {
			case pool == 0:
				f.N.SetDevice("PanPool", model.StateFault, "no clean pans")
			case pool <= 2:
				f.N.SetDevice("PanPool", model.StateWarn, fmt.Sprintf("%d clean pans", pool))
			default:
				f.N.SetDevice("PanPool", model.StateOK, fmt.Sprintf("%d clean pans", pool))
			}
			if pool <= 1 && f.F.Broken(washerMachine) {
				f.adm.Set(true, washReason)
				f.N.Warn("FC25", "asked depositing to stop new pans: "+washReason, "")
			}
			if pool >= 4 && !f.F.Broken(washerMachine) {
				f.adm.Set(false, washReason)
			}
		}
	}
}

// washer washes, dries and verifies empty pans (UC12, FC23).
func (f *Wash) washer() {
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
			f.took.Drop(id)
			go f.replace()
			continue
		}
		store.Move(f.Ctx, f.Pans, id, "FIN-PanPool", "clean", "washed and dried", func(p *model.Pan) { p.BatchID, p.FillWeight, p.CakeTemp = "", 0, 0 })
		f.N.AddGauge("washed", 1)
		f.mu.Lock()
		f.pool = append(f.pool, id)
		f.mu.Unlock()
		f.took.Drop(id)
	}
}

// supplier answers depositing's pan requests from the clean pool.
func (f *Wash) supplier() {
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
