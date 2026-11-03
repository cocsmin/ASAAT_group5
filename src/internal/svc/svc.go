// Package svc is the boilerplate every subsystem service shares: MQTT node,
// pan store, fault injection, repair handling and state snapshots.
package svc

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"cakefactory/internal/model"
	"cakefactory/internal/node"
	"cakefactory/internal/sig"
	"cakefactory/internal/sim"
	"cakefactory/internal/store"
	"cakefactory/internal/timing"
)

// Base is embedded by every subsystem.
type Base struct {
	ID    string
	Ctx   context.Context
	N     *node.Node
	F     *sim.Faults
	Pans  store.Pans
	mu    sync.Mutex
	repar map[string]func()
}

// Env returns an environment variable or a default.
func Env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// EnvDur reads a duration in seconds (may be fractional) or returns def.
func EnvDur(key string, def time.Duration) time.Duration {
	if f, err := strconv.ParseFloat(os.Getenv(key), 64); err == nil && f > 0 {
		return time.Duration(f * float64(time.Second))
	}
	return def
}

// Run connects the node and the pan store, loads the last saved state and
// returns the base. The context ends on SIGINT or SIGTERM.
func Run(id string, devices ...string) (*Base, context.CancelFunc) {
	log.SetFlags(log.Ltime | log.Lmicroseconds)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	pans, err := store.OpenPans(ctx)
	if err != nil {
		log.Fatal(err)
	}
	b := &Base{ID: id, Ctx: ctx, F: sim.NewFaults(), Pans: pans, repar: map[string]func(){}}
	b.N = node.New(node.Config{
		Broker:    Env("MQTT_BROKER", "tcp://localhost:1883"),
		ID:        id,
		Heartbeat: EnvDur("HEARTBEAT_SECONDS", 5*time.Second),
		Host:      id == model.MON,
		KeepAlive: EnvDur("MQTT_KEEPALIVE_S", 5*time.Second),
	})
	for _, d := range devices {
		b.N.SetDevice(d, model.StateOK, "")
	}
	if addr := os.Getenv("HEALTH_ADDR"); addr != "" {
		go func() { // container health check for Docker Compose
			mux := http.NewServeMux()
			mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) })
			_ = http.ListenAndServe(addr, mux)
		}()
	}
	b.N.OnCommand(sig.FaultInjection, b.onInject)
	b.N.OnCommand(sig.RepairConfirmed, b.onRepair)
	return b, cancel
}

// Start connects to the broker and starts the snapshot loop. Call it after all
// handlers have been registered.
func (b *Base) Start() {
	if st, err := b.Pans.LoadState(b.Ctx, b.ID); err == nil && st != nil {
		b.N.RestoreGauges(st)
		log.Printf("[%s] loaded last saved state (%d values)", b.ID, len(st))
		defer b.N.Log("FC27", "container restarted, last saved state loaded", "")
	}
	if err := b.N.Start(b.Ctx); err != nil {
		log.Fatal(err)
	}
	go func() {
		t := time.NewTicker(timing.StateSnapshot)
		defer t.Stop()
		for {
			select {
			case <-b.Ctx.Done():
				return
			case <-t.C:
				_ = b.Pans.SaveState(b.Ctx, b.ID, b.N.Gauges())
			}
		}
	}()
}

// OnRepair registers what happens when Maintenance confirms a repair of machine.
func (b *Base) OnRepair(machine string, fn func()) {
	b.mu.Lock()
	b.repar[machine] = fn
	b.mu.Unlock()
}

func (b *Base) controller() string { return strings.ToLower(b.ID) + "-controller" }

func (b *Base) onInject(s sig.Signal) {
	code, n := s.Str("code"), s.Int("count")
	if n <= 0 {
		n = 1
	}
	switch code {
	case "FC27":
		b.N.Warn("FC27", "controller container crashing now (injected)", "")
		go func() {
			time.Sleep(300 * time.Millisecond)
			os.Exit(1) // Docker restarts the container; state is reloaded
		}()
	case "FC28":
		b.F.Break(b.controller())
		b.N.Log("FC28", "controller will stop answering (injected)", "")
		go func() {
			time.Sleep(500 * time.Millisecond)
			b.N.SetSilent(true)
		}()
	default:
		b.F.Arm(code, n)
		log.Printf("[%s] fault %s armed x%d", b.ID, code, n)
	}
}

func (b *Base) onRepair(s sig.Signal) {
	m := s.Str("machineId")
	if m == b.controller() {
		b.F.Repair(m)
		b.N.SetSilent(false)
		b.N.Log("FC28", "controller answers again after repair", "")
		return
	}
	b.mu.Lock()
	fn := b.repar[m]
	b.mu.Unlock()
	b.F.Repair(m)
	if fn != nil {
		fn()
	}
}

// WaitRepaired blocks until Maintenance has confirmed the repair of machine.
// It returns false if the service is shutting down.
func (b *Base) WaitRepaired(machine string) bool {
	for b.F.Broken(machine) {
		select {
		case <-b.Ctx.Done():
			return false
		case <-time.After(150 * time.Millisecond):
		}
	}
	return true
}
