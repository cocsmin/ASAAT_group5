// Package sim holds the small pieces of simulation machinery shared by all
// services: scaled time, fault injection and pausable gates.
package sim

import (
	"context"
	"math/rand"
	"os"
	"strconv"
	"sync"
	"time"
)

var speed = func() float64 {
	if v, err := strconv.ParseFloat(os.Getenv("SIM_SPEED"), 64); err == nil && v > 0 {
		return v
	}
	return 1
}()

// Scale converts a simulated duration into wall-clock time.
func Scale(d time.Duration) time.Duration { return time.Duration(float64(d) / speed) }

// Unscale converts wall-clock time back into simulated time.
func Unscale(d time.Duration) time.Duration { return time.Duration(float64(d) * speed) }

// Sleep waits for a simulated duration. It returns false if ctx ended first.
func Sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(Scale(d))
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// Noise returns a normally distributed value with the given standard deviation.
func Noise(sigma float64) float64 { return rand.NormFloat64() * sigma }

// Chance returns true with probability p.
func Chance(p float64) bool { return rand.Float64() < p }

// Faults is the fault-injection registry of one service. A fault is "armed"
// with a number of charges; each occurrence in the simulation consumes one.
// Broken machines stay broken until repaired.
type Faults struct {
	mu      sync.Mutex
	charges map[string]int
	broken  map[string]bool
}

// NewFaults creates an empty registry.
func NewFaults() *Faults {
	return &Faults{charges: map[string]int{}, broken: map[string]bool{}}
}

// Arm adds charges for a fault code.
func (f *Faults) Arm(code string, n int) {
	f.mu.Lock()
	f.charges[code] += n
	f.mu.Unlock()
}

// Take consumes one charge and reports whether the fault fires now.
func (f *Faults) Take(code string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.charges[code] > 0 {
		f.charges[code]--
		return true
	}
	return false
}

// Armed reports whether charges remain, without consuming one.
func (f *Faults) Armed(code string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.charges[code] > 0
}

// Break marks a machine as broken until Repair is called.
func (f *Faults) Break(machine string) {
	f.mu.Lock()
	f.broken[machine] = true
	f.mu.Unlock()
}

// Repair clears a broken machine and reports whether it had been broken.
func (f *Faults) Repair(machine string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	was := f.broken[machine]
	delete(f.broken, machine)
	return was
}

// Broken reports whether a machine is broken.
func (f *Faults) Broken(machine string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.broken[machine]
}

// Gate lets goroutines pass while open and blocks them while closed.
type Gate struct {
	mu sync.Mutex
	ch chan struct{} // closed while the gate is open
}

// NewGate creates a gate, initially open.
func NewGate() *Gate {
	g := &Gate{ch: make(chan struct{})}
	close(g.ch)
	return g
}

// Set opens or closes the gate.
func (g *Gate) Set(open bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	select {
	case <-g.ch:
		if !open {
			g.ch = make(chan struct{})
		}
	default:
		if open {
			close(g.ch)
		}
	}
}

// Open reports whether the gate is open.
func (g *Gate) Open() bool {
	g.mu.Lock()
	ch := g.ch
	g.mu.Unlock()
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

// Wait blocks until the gate is open. It returns false if ctx ended first.
func (g *Gate) Wait(ctx context.Context) bool {
	for {
		g.mu.Lock()
		ch := g.ch
		g.mu.Unlock()
		select {
		case <-ctx.Done():
			return false
		case <-ch:
			if g.Open() {
				return true
			}
		}
	}
}
