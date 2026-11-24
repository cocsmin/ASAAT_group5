package fin

import (
	"log"
	"strings"
	"sync/atomic"
	"time"

	"cakefactory/internal/model"
	"cakefactory/internal/sig"
	"cakefactory/internal/sim"
	"cakefactory/internal/station"
	"cakefactory/internal/store"
	"cakefactory/internal/svc"
)

// Dpn is the depanning station: it separates cake and pan (UC11, FC21, FC22).
type Dpn struct {
	*svc.Base
	sec    *station.Section
	took   *station.PanSet
	cooled chan cake
	halted atomic.Bool // band halt: finish product inside, accept no new product
}

// RunDpn starts the depanning station.
func RunDpn() {
	b, cancel := svc.Run(model.FINDpn, "Depanner", "VisionPicker")
	defer cancel()
	f := &Dpn{Base: b, sec: station.NewSection(4), took: station.NewPanSet(), cooled: make(chan cake, 1024)}
	f.N.OnSignal(model.THMCool, sig.CooledCake, func(_ string, s sig.Signal) {
		if s.Float("temp") > model.TSafe { // only cakes released at a safe temperature are accepted (FIN-REQ-02)
			f.N.Warn("UC10", "cake not released at a safe temperature: not accepted", s.Str("batchId"))
			return
		}
		f.enqueue(cake{pan: s.Str("panId"), batch: s.Str("batchId")})
	})
	f.N.OnSignal(model.BT, sig.BandState, func(_ string, s sig.Signal) {
		halted := f.sec.OnBand(s)
		if f.halted.Swap(halted) != halted {
			f.N.Warn("UC2", map[bool]string{true: "band state: halt: finish product inside, accept no new product", false: "band state: running again"}[halted], "")
		}
	})
	f.OnRepair(depannerMachine, func() {
		f.N.SetDevice("Depanner", model.StateOK, "")
		f.N.Log("FC22", "depanning machine repaired: held cakes are processed", "")
	})
	f.Start()
	station.Reconcile(f.Base, f.adopt)
	f.depanner()
}

func (f *Dpn) enqueue(c cake) {
	if f.took.Add(c.batch + "/" + c.pan) {
		f.cooled <- c
		f.N.Gauge("depanQ", int64(len(f.cooled)))
	}
}

// adopt takes up the cakes that cooling released without a message arriving, and
// the ones that were waiting here when this container restarted.
func (f *Dpn) adopt() {
	all, err := f.Pans.List(f.Ctx, 1000)
	if err != nil {
		return
	}
	n := 0
	for i := len(all) - 1; i >= 0; i-- {
		p := all[i]
		if !strings.HasPrefix(p.ID, "CAKE-") && p.Status == "cooled" && p.Station == "THM-Cooling" && time.Since(p.Updated) > station.HandoverGrace {
			if f.took.Add(p.BatchID + "/" + p.ID) {
				f.cooled <- cake{pan: p.ID, batch: p.BatchID}
				n++
			}
		}
	}
	if n > 0 {
		log.Printf("[%s] %d cakes taken up from the stored documents", f.ID, n)
	}
}

func (f *Dpn) depanner() {
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
			f.N.Gauge("depanQ", int64(len(f.cooled)))
		}
		if f.F.Take("FC22") && !f.F.Broken(depannerMachine) {
			f.F.Break(depannerMachine)
			f.N.SetDevice("Depanner", model.StateFault, "not responding")
			f.N.Fault("FC22", "depanning machine stopped responding: cakes are held, the cake in the machine is removed by hand", c.batch, "Depanner", depannerMachine)
		}
		if !f.WaitRepaired(depannerMachine) || !f.sec.Span(f.Base, depanTime) {
			return
		}
		key := c.batch + "/" + c.pan
		if f.F.Take("FC21") {
			f.N.SetDevice("VisionPicker", model.StateWarn, "cake not removed correctly")
			store.Move(f.Ctx, f.Pans, c.pan, "FIN-Depanning", "emptied", "cake stuck or broken", nil)
			f.N.Discard("FC21", "cake stuck or broken in its pan: cake rejected, pan sent to washing", c.batch)
			f.N.AddGauge("rejected", 1)
			f.N.SendReliable(sig.New(sig.PanForWashing, "panId", c.pan, "batchId", c.batch, "reason", "FC21"))
			f.took.Drop(key)
			continue
		}
		f.N.SetDevice("VisionPicker", model.StateOK, "")
		store.Move(f.Ctx, f.Pans, c.pan, "FIN-Depanning", "depanned", "", nil)
		f.N.AddGauge("depanned", 1)
		f.N.SendReliable(sig.New(sig.PanForWashing, "panId", c.pan, "batchId", c.batch, "reason", "depanned")) // the empty pan goes to the washing station
		store.Move(f.Ctx, f.Pans, c.doc(), "FIN-Depanning", "awaiting inspection", "", func(p *model.Pan) { p.PanID, p.BatchID = c.pan, c.batch })
		f.N.SendReliable(sig.New(sig.CakeToInspect, "panId", c.pan, "batchId", c.batch))
		f.took.Drop(key)
	}
}
