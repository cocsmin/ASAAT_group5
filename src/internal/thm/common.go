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
	"time"

	"cakefactory/internal/sig"
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

// divert sends an emptied pan to washing and records a discard.
func divert(b *svc.Base, p pan, code, reason string) {
	store.Move(b.Ctx, b.Pans, p.id, "THM", "discarded", reason, nil)
	b.N.Discard(code, reason, p.batch)
	b.N.AddGauge("discarded", 1)
	b.N.SendReliable(sig.New(sig.PanForWashing, "panId", p.id, "batchId", p.batch, "reason", code))
}
