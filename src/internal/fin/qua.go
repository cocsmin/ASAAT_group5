package fin

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"cakefactory/internal/model"
	"cakefactory/internal/sig"
	"cakefactory/internal/station"
	"cakefactory/internal/store"
	"cakefactory/internal/svc"
)

// Qua is the quality inspection station: weight, appearance and metal (UC13, FC24).
type Qua struct {
	*svc.Base
	sec       *station.Section
	took      *station.PanSet
	toInspect chan cake

	mu          sync.Mutex
	rejectRun   int
	rejectCheck string
}

// RunQua starts the quality inspection station.
func RunQua() {
	b, cancel := svc.Run(model.FINQua, "Checkweigher", "VisionCamera", "MetalDetector")
	defer cancel()
	f := &Qua{Base: b, sec: station.NewSection(4), took: station.NewPanSet(), toInspect: make(chan cake, 1024)}
	f.N.OnSignal(model.FINDpn, sig.CakeToInspect, func(_ string, s sig.Signal) {
		f.enqueue(cake{pan: s.Str("panId"), batch: s.Str("batchId")})
	})
	f.N.OnSignal(model.BT, sig.BandState, func(_ string, s sig.Signal) { f.sec.OnBand(s) })
	f.OnRepair(inspectorMachine, func() {
		f.N.SetDevice("Checkweigher", model.StateOK, "")
		f.N.Log("FC24", "inspection equipment checked and repaired", "")
	})
	f.Start()
	station.Reconcile(f.Base, f.adopt)
	f.inspector()
}

func (f *Qua) enqueue(c cake) {
	if f.took.Add(c.doc()) {
		f.toInspect <- c
	}
}

// adopt takes up the cakes that depanning handed over without a message arriving.
func (f *Qua) adopt() {
	all, err := f.Pans.List(f.Ctx, 1000)
	if err != nil {
		return
	}
	n := 0
	for i := len(all) - 1; i >= 0; i-- {
		p := all[i]
		if strings.HasPrefix(p.ID, "CAKE-") && p.Status == "awaiting inspection" && time.Since(p.Updated) > station.HandoverGrace {
			c := cake{pan: p.PanID, batch: p.BatchID}
			if f.took.Add(c.doc()) {
				f.toInspect <- c
				n++
			}
		}
	}
	if n > 0 {
		log.Printf("[%s] %d cakes taken up from the stored documents", f.ID, n)
	}
}

func (f *Qua) inspector() {
	var sticky string // while a fault is armed the same check keeps failing
	for {
		var c cake
		select {
		case <-f.Ctx.Done():
			return
		case c = <-f.toInspect:
		}
		if !f.WaitRepaired(inspectorMachine) || !f.sec.Span(f.Base, inspectTime) {
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
			f.N.SendReliable(sig.New(sig.CakeToWrapping, "panId", c.pan, "batchId", c.batch))
			f.took.Drop(c.doc())
			continue
		}
		store.Move(f.Ctx, f.Pans, c.doc(), "FIN-Inspection", "rejected", reason, nil)
		f.N.AddGauge("rejected", 1)
		f.N.Discard("FC24", "cake rejected at quality inspection: "+reason, c.batch)
		f.took.Drop(c.doc())
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
