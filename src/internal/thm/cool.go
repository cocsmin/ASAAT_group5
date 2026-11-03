package thm

import (
	"fmt"
	"log"
	"math"
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

type bufItem struct {
	pan   pan
	temp  float64
	since time.Time
}

// Cool is the cooling station with its fallback buffer on a separate level.
type Cool struct {
	*svc.Base
	sec  *section
	adm  *admission
	took *panSet

	mu         sync.Mutex
	buf        []bufItem
	coolFailAt time.Time
	expireAll  bool
	inCooling  atomic.Int64
}

// RunCool starts the cooling station.
func RunCool() {
	b, cancel := svc.Run(model.THMCool, "CoolingControl", "CoolingBuffer")
	defer cancel()
	t := &Cool{Base: b, sec: newSection(3), took: newPanSet()}
	t.adm = &admission{b: b}
	t.N.OnSignal(model.THMExit, sig.PanToCooling, func(_ string, s sig.Signal) {
		t.take(pan{id: s.Str("panId"), batch: s.Str("batchId")}, s.Float("temp"))
	})
	t.N.OnSignal(model.BT, sig.BandState, func(_ string, s sig.Signal) { t.sec.onBand(s) })
	t.OnRepair(coolingMachine, t.coolingRepaired)
	t.restoreBuffered()
	t.Start()
	t.N.SetState(sig.New(sig.PanAdmission, "stop", false, "reason", "")) // after a restart the buffer may have room again
	go t.releaseLoop()
	go t.gaugeLoop()
	reconcile(t.Base, t.adopt)
	<-t.Ctx.Done()
}

func (t *Cool) take(p pan, temp float64) {
	if t.took.add(p.id) {
		go t.cool(p, temp)
	}
}

// adopt takes up the cakes that the tunnel exit passed without a message arriving,
// and the cakes that were inside the cooling zone when this container restarted.
func (t *Cool) adopt() {
	all, err := t.Pans.List(t.Ctx, 1000)
	if err != nil {
		return
	}
	n := 0
	for i := len(all) - 1; i >= 0; i-- {
		p := all[i]
		c := pan{id: p.ID, batch: p.BatchID}
		switch {
		case p.Status == "baked" && p.Station == "THM-TunnelExit" && time.Since(p.Updated) > handoverGrace:
			if !t.took.add(c.id) {
				continue
			}
			go t.cool(c, p.CakeTemp)
		case p.Status == "cooling" && p.Station == "THM-Cooling":
			if !t.took.add(c.id) {
				continue
			}
			go t.cool(c, p.CakeTemp)
		default:
			continue
		}
		n++
	}
	if n > 0 {
		log.Printf("[%s] %d cakes taken up from the stored documents", t.ID, n)
	}
}

func (t *Cool) gaugeLoop() {
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-t.Ctx.Done():
			return
		case <-tick.C:
			t.N.Gauge("cooling", t.inCooling.Load())
			t.mu.Lock()
			t.N.Gauge("buffer", int64(len(t.buf)))
			t.mu.Unlock()
		}
	}
}

// cool sends the cake through the cooling zone or, if cooling is out of
// service or the cake is still too hot, into the cooling buffer.
func (t *Cool) cool(p pan, temp float64) {
	defer t.took.drop(p.id)
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
	ok := t.sec.span(t.Base, coolingTime)
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

func (t *Cool) deliver(p pan, temp float64) {
	store.Move(t.Ctx, t.Pans, p.id, "THM-Cooling", "cooled", fmt.Sprintf("core %.1f C", temp), func(m *model.Pan) { m.CakeTemp = temp })
	t.N.AddGauge("cooled", 1)
	t.N.SendReliable(sig.New(sig.CooledCake, "panId", p.id, "batchId", p.batch, "temp", temp))
}

// buffer holds a cake in the separate-level cooling buffer (FC15, FC14).
func (t *Cool) buffer(p pan, temp float64, why string) {
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
		t.adm.set(true, "cooling buffer full (FC18)")
		divert(t.Base, p, "FC18", "cooling buffer full: cake diverted to refuse")
		return
	}
	store.Move(t.Ctx, t.Pans, p.id, "THM-CoolingBuffer", "buffered", why, nil)
	t.N.SetDevice("CoolingBuffer", model.StateWarn, fmt.Sprintf("%d cakes held", n))
	t.N.Log("FC15", fmt.Sprintf("cake held in cooling buffer (%s), %d/%d", why, n, bufferCapacity), p.batch)
	if n >= bufferCapacity { // FC18: report "buffer full" at once, stop new pans, the exit diverts what still comes
		t.adm.set(true, "cooling buffer full (FC18)")
		t.N.SetDevice("CoolingBuffer", model.StateFault, "full")
		t.N.Emit(node.Event{Severity: model.SevError, Code: "FC18", BatchID: p.batch,
			Reason: fmt.Sprintf("cooling buffer full (%d/%d): new pans stopped, cakes leaving the tunnel are diverted to refuse", n, bufferCapacity)})
	}
}

func (t *Cool) coolingRepaired() {
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
		t.adm.set(false, "")
	}
}

// releaseLoop releases buffered cakes once cooling works (FC16) and discards
// cakes that waited too long (FC17).
func (t *Cool) releaseLoop() {
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
			divert(t.Base, it.pan, "FC17", fmt.Sprintf("cake waited %.0f s in the cooling buffer (limit %.0f s): discarded", sim.Unscale(time.Since(it.since)).Seconds(), maxBufferWait.Seconds()))
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
				t.adm.set(false, "")
			}
		}
	}
}

// restoreBuffered puts the cakes of the cooling buffer back. The buffer lives on
// a separate level, so the cakes are still there after a restart, and their
// waiting time continues from the time they were put in (THM deployment table:
// "cooling-buffer waiting times are kept in saved state").
func (t *Cool) restoreBuffered() {
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
		log.Printf("[%s] restored %d cakes of the cooling buffer", t.ID, len(t.buf))
		t.N.SetDevice("CoolingBuffer", model.StateWarn, fmt.Sprintf("%d cakes held", len(t.buf)))
	}
}
