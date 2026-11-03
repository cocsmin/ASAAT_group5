package thm

import (
	"fmt"
	"log"
	"math"
	"sync/atomic"
	"time"

	"cakefactory/internal/model"
	"cakefactory/internal/sig"
	"cakefactory/internal/sim"
	"cakefactory/internal/store"
	"cakefactory/internal/svc"
)

// Exit is the tunnel exit station: the heat camera checks every cake.
type Exit struct {
	*svc.Base
	sec        *section
	took       *panSet
	bufferFull atomic.Bool // cooling reports that its buffer is full (FC18)
}

// RunExit starts the tunnel exit station.
func RunExit() {
	b, cancel := svc.Run(model.THMExit, "HeatCamera")
	defer cancel()
	t := &Exit{Base: b, sec: newSection(3), took: newPanSet()}
	t.N.OnSignal(model.THMBake, sig.PanToExit, func(_ string, s sig.Signal) {
		t.take(pan{id: s.Str("panId"), batch: s.Str("batchId"), off: s.Float("off")})
	})
	t.N.OnSignal(model.THMCool, sig.PanAdmission, func(_ string, s sig.Signal) { t.bufferFull.Store(s.Bool("stop")) })
	t.N.OnSignal(model.BT, sig.BandState, func(_ string, s sig.Signal) { t.sec.onBand(s) })
	t.OnRepair(cameraMachine, func() {
		t.N.SetDevice("HeatCamera", model.StateOK, "")
		t.N.Log("FC13", "heat camera repaired: normal checking resumes", "")
	})
	t.Start()
	reconcile(t.Base, t.adopt)
	<-t.Ctx.Done()
}

func (t *Exit) take(p pan) {
	if t.took.add(p.id) {
		go t.check(p)
	}
}

// adopt takes up the pans that left the tunnel without a message arriving.
func (t *Exit) adopt() {
	all, err := t.Pans.List(t.Ctx, 1000)
	if err != nil {
		return
	}
	n := 0
	for i := len(all) - 1; i >= 0; i-- {
		if p := all[i]; p.Status == "tunnel-exit" && p.Station == "THM-Baking" && time.Since(p.Updated) > handoverGrace && t.took.add(p.ID) {
			go t.check(pan{id: p.ID, batch: p.BatchID})
			n++
		}
	}
	if n > 0 {
		log.Printf("[%s] %d pans taken up from the stored documents", t.ID, n)
	}
}

func (t *Exit) check(p pan) {
	defer t.took.drop(p.id)
	if !t.sec.span(t.Base, exitCheckTime) {
		return
	}
	if t.F.Take("FC13") && !t.F.Broken(cameraMachine) {
		t.F.Break(cameraMachine)
		t.N.SetDevice("HeatCamera", model.StateFault, "not responding")
		t.N.Fault("FC13", "heat camera stopped responding: all cakes are diverted until it is repaired", p.batch, "HeatCamera", cameraMachine)
	}
	if t.F.Broken(cameraMachine) {
		divert(t.Base, p, "FC13", "heat camera not responding: cake cannot be checked, diverted to refuse")
		return
	}
	if t.bufferFull.Load() { // buffer full: the exit diverts all cakes until it has free places (THM-REQ-58, -60)
		divert(t.Base, p, "FC18", "cooling buffer full: tunnel exit diverts all cakes")
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
		divert(t.Base, p, "FC12", fmt.Sprintf("cake %s: %.2f C, allowed %.1f ± %.1f C", kind, temp, bakeTarget, bakeTol))
		return
	}
	t.N.AddGauge("baked", 1)
	t.N.SendReliable(sig.New(sig.PanToCooling, "panId", p.id, "batchId", p.batch, "temp", temp))
}
