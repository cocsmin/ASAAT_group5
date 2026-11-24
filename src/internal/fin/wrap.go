package fin

import (
	"log"
	"strings"
	"time"

	"cakefactory/internal/model"
	"cakefactory/internal/sig"
	"cakefactory/internal/station"
	"cakefactory/internal/store"
	"cakefactory/internal/svc"
)

const holdingReason = "wrapping station down, holding area full"

// Wrap is the wrapping station with the holding area in front of it (UC14, FC25).
type Wrap struct {
	*svc.Base
	sec    *station.Section
	adm    *station.Admission
	took   *station.PanSet
	toWrap chan cake // the holding area
}

// RunWrap starts the wrapping station.
func RunWrap() {
	b, cancel := svc.Run(model.FINWrap, "Wrapper", "HoldingArea")
	defer cancel()
	f := &Wrap{Base: b, sec: station.NewSection(5), took: station.NewPanSet(), toWrap: make(chan cake, 1024)}
	f.adm = &station.Admission{B: b}
	f.N.OnSignal(model.FINQua, sig.CakeToWrapping, func(_ string, s sig.Signal) {
		f.enqueue(cake{pan: s.Str("panId"), batch: s.Str("batchId")})
	})
	f.N.OnSignal(model.BT, sig.BandState, func(_ string, s sig.Signal) { f.sec.OnBand(s) })
	f.OnRepair(wrapperMachine, func() {
		f.N.SetDevice("Wrapper", model.StateOK, "")
		f.N.Log("FC25", "wrapping station repaired: held cakes are wrapped", "")
	})
	f.Start()
	f.adm.Reset()
	go f.gaugeLoop()
	station.Reconcile(f.Base, f.adopt)
	f.wrapper()
}

func (f *Wrap) enqueue(c cake) {
	if f.took.Add(c.doc()) {
		f.toWrap <- c
	}
}

// adopt takes up the cakes that inspection passed without a message arriving.
func (f *Wrap) adopt() {
	all, err := f.Pans.List(f.Ctx, 1000)
	if err != nil {
		return
	}
	n := 0
	for i := len(all) - 1; i >= 0; i-- {
		p := all[i]
		if strings.HasPrefix(p.ID, "CAKE-") && p.Status == "inspected" && time.Since(p.Updated) > station.HandoverGrace {
			c := cake{pan: p.PanID, batch: p.BatchID}
			if f.took.Add(c.doc()) {
				f.toWrap <- c
				n++
			}
		}
	}
	if n > 0 {
		log.Printf("[%s] %d cakes taken up from the stored documents", f.ID, n)
	}
}

// gaugeLoop watches the holding area: when it is full, or the wrapper is down and it
// is nearly full, depositing is asked to stop new pans.
func (f *Wrap) gaugeLoop() {
	t := time.NewTicker(500 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-f.Ctx.Done():
			return
		case <-t.C:
			holding := len(f.toWrap)
			f.N.Gauge("holding", int64(holding))
			if holding >= holdingCap {
				f.N.SetDevice("HoldingArea", model.StateWarn, "full")
			} else {
				f.N.SetDevice("HoldingArea", model.StateOK, "")
			}
			if holding >= holdingCap || (holding >= holdingCap-1 && f.F.Broken(wrapperMachine)) {
				f.adm.Set(true, holdingReason)
			}
			if holding <= 1 && !f.F.Broken(wrapperMachine) {
				f.adm.Set(false, holdingReason)
			}
		}
	}
}

func (f *Wrap) wrapper() {
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
		if !f.WaitRepaired(wrapperMachine) || !f.sec.Span(f.Base, wrapTime) {
			return
		}
		if f.F.Take("UC14") { // wrap or carton check fails: refuse bin, only properly wrapped cakes ship
			store.Move(f.Ctx, f.Pans, c.doc(), "FIN-Wrapping", "refused", "wrap or carton defective", nil)
			f.N.Discard("UC14", "wrapped or cartoned cake defective: moved to refuse bin", c.batch)
			f.N.AddGauge("rejected", 1)
			f.took.Drop(c.doc())
			continue
		}
		store.Move(f.Ctx, f.Pans, c.doc(), "FIN-Wrapping", "wrapped", "", nil)
		f.N.SendReliable(sig.New(sig.CakeToCartoning, "panId", c.pan, "batchId", c.batch)) // cartoning and case check happen at the pallet station
		f.took.Drop(c.doc())
	}
}
