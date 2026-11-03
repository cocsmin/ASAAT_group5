// Package station holds the pieces every per-station container shares: the band
// section it works on, the set of pans it has taken up, the periodic scan for pans
// that were handed over without a message arriving, and the stop/resume signal for
// pan admission. Thermal Processing and Finishing & Packaging are built from it.
package station

import (
	"sync"
	"sync/atomic"
	"time"

	"cakefactory/internal/sig"
	"cakefactory/internal/sim"
	"cakefactory/internal/svc"
)

const (
	// HandoverGrace is how long a handed-over pan waits before the next station adopts it
	// from its document: the message wins, and a pan that is diverted at once is never taken up.
	HandoverGrace = 3 * time.Second
	// ReconcileEvery is how often a station looks for pans handed over without a message.
	ReconcileEvery = 5 * time.Second
)

// Section is the band section a station works on: the station waits while the
// section does not run and works at half speed while it is slowed down.
type Section struct {
	N    int
	Gate *sim.Gate
	slow atomic.Bool
}

// NewSection creates the section n (1 to 5), initially running.
func NewSection(n int) *Section { return &Section{N: n, Gate: sim.NewGate()} }

// OnBand applies "Band state change" and reports whether the section is halted.
func (s *Section) OnBand(b sig.Signal) (halted bool) {
	v := b.Str("S" + string(rune('0'+s.N)))
	s.Gate.Set(v != "halt")
	s.slow.Store(v == "slow")
	return v == "halt"
}

// Span waits for the section to run and then simulates d of work.
func (s *Section) Span(b *svc.Base, d time.Duration) bool {
	if !s.Gate.Wait(b.Ctx) {
		return false
	}
	if s.slow.Load() {
		d *= 2
	}
	return sim.Sleep(b.Ctx, d)
}

// PanSet remembers which pans a station has taken up, so a message and the
// reconcile scan never start the same pan twice.
type PanSet struct {
	mu sync.Mutex
	m  map[string]bool
}

// NewPanSet creates an empty set.
func NewPanSet() *PanSet { return &PanSet{m: map[string]bool{}} }

// Add returns false when the key was already taken.
func (p *PanSet) Add(key string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.m[key] {
		return false
	}
	p.m[key] = true
	return true
}

// Drop forgets a key.
func (p *PanSet) Drop(key string) {
	p.mu.Lock()
	delete(p.m, key)
	p.mu.Unlock()
}

// Reconcile calls scan now and then every ReconcileEvery.
func Reconcile(b *svc.Base, scan func()) {
	scan()
	go func() {
		t := time.NewTicker(ReconcileEvery)
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

// Admission is the state signal that tells the depositing station to stop or
// resume. It only publishes when the state changes.
type Admission struct {
	B       *svc.Base
	mu      sync.Mutex
	stopped bool
}

// Set stops (stop = true) or resumes pan admission.
func (a *Admission) Set(stop bool, reason string) {
	a.mu.Lock()
	changed := a.stopped != stop
	a.stopped = stop
	a.mu.Unlock()
	if changed {
		a.B.N.SetState(sig.New(sig.PanAdmission, "stop", stop, "reason", reason))
	}
}

// Reset publishes "resume" without a change check: a station that restarts calls it
// so a stop that it sent before the restart does not stay in force.
func (a *Admission) Reset() {
	a.mu.Lock()
	a.stopped = false
	a.mu.Unlock()
	a.B.N.SetState(sig.New(sig.PanAdmission, "stop", false, "reason", ""))
}
