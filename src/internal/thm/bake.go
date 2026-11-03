package thm

import (
	"fmt"
	"log"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"cakefactory/internal/model"
	"cakefactory/internal/sig"
	"cakefactory/internal/sim"
	"cakefactory/internal/store"
	"cakefactory/internal/svc"
)

// Bake is the baking tunnel station: three heating zones on band section 2.
type Bake struct {
	*svc.Base
	sec  *section
	adm  *admission
	took *panSet

	mu        sync.Mutex
	zoneOff   [4]float64
	zoneSince time.Time
	zoneFault bool
	inTunnel  atomic.Int64
}

// RunBake starts the baking tunnel station.
func RunBake() {
	b, cancel := svc.Run(model.THMBake, "Zone1", "Zone2", "Zone3")
	defer cancel()
	t := &Bake{Base: b, sec: newSection(2), took: newPanSet()}
	t.adm = &admission{b: b}
	t.N.OnSignal(model.THMDep, sig.PanToBaking, func(_ string, s sig.Signal) {
		t.take(pan{id: s.Str("panId"), batch: s.Str("batchId")})
	})
	t.N.OnSignal(model.BT, sig.BandState, func(_ string, s sig.Signal) { t.sec.onBand(s) })
	t.OnRepair(zoneMachine, t.zoneRepaired)
	t.Start()
	t.N.SetState(sig.New(sig.PanAdmission, "stop", false, "reason", "")) // after a restart the zone is no longer flagged
	go t.zoneLoop()
	go t.gaugeLoop()
	reconcile(t.Base, t.adopt)
	<-t.Ctx.Done()
}

// take starts a pan through the tunnel unless it is already in it.
func (t *Bake) take(p pan) {
	if t.took.add(p.id) {
		go t.bake(p)
	}
}

// bake moves a pan through the three zones and hands it to the tunnel exit.
func (t *Bake) bake(p pan) {
	defer t.took.drop(p.id)
	t.inTunnel.Add(1)
	store.Move(t.Ctx, t.Pans, p.id, "THM-Baking", "baking", "", nil)
	for z := 1; z <= 3; z++ {
		if !t.sec.span(t.Base, zoneTransit) {
			t.inTunnel.Add(-1)
			return
		}
		t.mu.Lock()
		p.off += t.zoneOff[z]
		t.mu.Unlock()
	}
	t.inTunnel.Add(-1)
	store.Move(t.Ctx, t.Pans, p.id, "THM-Baking", "tunnel-exit", fmt.Sprintf("offset %.1f", p.off), nil)
	t.N.SendReliable(sig.New(sig.PanToExit, "panId", p.id, "batchId", p.batch, "off", p.off))
}

// adopt takes up the pans that depositing handed over without a message arriving
// (this container was down, or the message was lost) and the pans that were inside
// the tunnel when it restarted. The heaters kept their last command, so the pans
// simply continue from the stage they were at.
func (t *Bake) adopt() {
	all, err := t.Pans.List(t.Ctx, 1000)
	if err != nil {
		return
	}
	n := 0
	for i := len(all) - 1; i >= 0; i-- {
		p := all[i]
		c := pan{id: p.ID, batch: p.BatchID}
		switch {
		case p.Status == "filled" && p.Station == "THM-Depositing" && time.Since(p.Updated) > handoverGrace:
			if !t.took.add(c.id) {
				continue
			}
			if math.Abs(p.FillWeight-store.SeedRecipe.FillTarget) > store.SeedRecipe.FillTol {
				t.took.drop(c.id)
				divert(t.Base, c, "FC9", fmt.Sprintf("restart: fill weight %.1f g was out of tolerance: pan diverted", p.FillWeight))
				continue
			}
			go t.bake(c)
		case p.Status == "baking" && p.Station == "THM-Baking":
			if !t.took.add(c.id) {
				continue
			}
			go t.bake(c) // the pan continues through the zones
		default:
			continue
		}
		n++
	}
	if n > 0 {
		log.Printf("[%s] %d pans taken up from the stored documents", t.ID, n)
		t.N.Log("FC27", fmt.Sprintf("%d pans in or before the tunnel taken up again from the stored documents", n), "")
	}
}

func (t *Bake) gaugeLoop() {
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-t.Ctx.Done():
			return
		case <-tick.C:
			t.N.Gauge("tunnel", t.inTunnel.Load())
		}
	}
}

// zoneLoop simulates the zone temperature control loops (FC11).
func (t *Bake) zoneLoop() {
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
			t.adm.set(true, "baking zone 2 faulty (FC11)")
			t.N.Fault("FC11", "zone 2 still out of range after adjustment: flagged faulty, new pans stopped; pans inside finish and are checked at the exit",
				"", "Zone2", zoneMachine)
		}
	}
}

func (t *Bake) zoneRepaired() {
	t.mu.Lock()
	t.zoneOff[2] = 0
	t.zoneFault = false
	t.mu.Unlock()
	t.N.SetDevice("Zone2", model.StateOK, "")
	t.adm.set(false, "")
	t.N.Log("FC11", "zone 2 repaired: depositing resumes", "")
}
