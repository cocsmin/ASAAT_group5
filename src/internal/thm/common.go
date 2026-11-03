// Package thm is the Thermal Processing system. It is split per station, one
// container each: Depositing (dep.go), Baking tunnel (bake.go), Tunnel exit check
// (exit.go) and Cooling with its fallback buffer (cool.go). The stations hand a pan
// on with a signal ("Pan to baking", "Pan to tunnel exit", "Pan to cooling"); the
// pan document in MongoDB says where the pan is, so a station that restarts, or
// that missed a message, adopts the pans the station before it has handed over
// (adopt in every station). A station that cannot take more pans tells the
// depositing station with "Stop / resume pan admission".
package thm

import (
	"sync"
	"sync/atomic"
	"time"

	"cakefactory/internal/sig"
	"cakefactory/internal/sim"
	"cakefactory/internal/store"
	"cakefactory/internal/svc"
	"cakefactory/internal/timing"
)

// Parameters (simulated time and degrees Celsius).
const (
	nWrong         = 3               // N_wrong: fills out of tolerance in a row
	tAdjust        = 4 * time.Second // T_adjust
	depositTime    = 1 * time.Second
	zoneTransit    = 2 * time.Second
	exitCheckTime  = 500 * time.Millisecond
	coolingTime    = 4 * time.Second
	maxBufferWait  = timing.BufferWait // stands for the 30 minutes of UC10
	bufferCapacity = 30
	tSafe          = 30.0
	bakeTarget     = 97.0
	bakeTol        = 0.5

	depositorMachine = "thm-dep-depositor"
	zoneMachine      = "thm-bake-zone-2"
	cameraMachine    = "thm-exit-heatcamera"
	coolingMachine   = "thm-cool-cooling"

	handoverGrace  = 3 * time.Second // a handed-over pan is adopted by the next station only after this, so the message wins and a pan that is diverted at once is never taken up
	reconcileEvery = 5 * time.Second // how often a station looks for pans that were handed over without a message arriving
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

// section is the band section a station works on: it waits while the section does
// not run and works at half speed while it is slowed down.
type section struct {
	n    int
	gate *sim.Gate
	slow atomic.Bool
}

func newSection(n int) *section { return &section{n: n, gate: sim.NewGate()} }

// onBand applies "Band state change" for this station's section. It returns true
// when the section is halted.
func (s *section) onBand(b sig.Signal) (halted bool) {
	v := b.Str(sectionKey(s.n))
	s.gate.Set(v != "halt")
	s.slow.Store(v == "slow")
	return v == "halt"
}

func sectionKey(n int) string { return "S" + string(rune('0'+n)) }

// span waits for the band section to run and then simulates d of work.
func (s *section) span(b *svc.Base, d time.Duration) bool {
	if !s.gate.Wait(b.Ctx) {
		return false
	}
	if s.slow.Load() {
		d *= 2
	}
	return sim.Sleep(b.Ctx, d)
}

// divert sends an emptied pan to washing and records a discard.
func divert(b *svc.Base, p pan, code, reason string) {
	store.Move(b.Ctx, b.Pans, p.id, "THM", "discarded", reason, nil)
	b.N.Discard(code, reason, p.batch)
	b.N.AddGauge("discarded", 1)
	b.N.SendReliable(sig.New(sig.PanForWashing, "panId", p.id, "batchId", p.batch, "reason", code))
}

// panSet remembers which pans a station has taken up, so a message and the
// reconcile scan never start the same pan twice.
type panSet struct {
	mu sync.Mutex
	m  map[string]bool
}

func newPanSet() *panSet { return &panSet{m: map[string]bool{}} }

// add returns false when the key was already taken.
func (p *panSet) add(key string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.m[key] {
		return false
	}
	p.m[key] = true
	return true
}

func (p *panSet) drop(key string) {
	p.mu.Lock()
	delete(p.m, key)
	p.mu.Unlock()
}

// reconcile calls scan now and then every reconcileEvery.
func reconcile(b *svc.Base, scan func()) {
	scan()
	go func() {
		t := time.NewTicker(reconcileEvery)
		defer t.Stop()
		for {
			select {
			case <-b.Ctx.Done():
				return
			case <-t.C:
				scan()
			}
		}
	}()
}

// admission is a state signal that tells the depositing station to stop or
// resume. It only publishes when the state changes.
type admission struct {
	b       *svc.Base
	mu      sync.Mutex
	stopped bool
}

func (a *admission) set(stop bool, reason string) {
	a.mu.Lock()
	changed := a.stopped != stop
	a.stopped = stop
	a.mu.Unlock()
	if changed {
		a.b.N.SetState(sig.New(sig.PanAdmission, "stop", stop, "reason", reason))
	}
}
