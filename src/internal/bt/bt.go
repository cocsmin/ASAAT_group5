// Package bt is the Band Transport system (BT-AF-01 Section Control, BT-AF-02
// Speed Monitoring, BT-AF-03 Transport Fault Handling) with its devices
// (conveyor controller, conveyor drive, speed sensor, section status sensor).
//
// It owns the five independently drivable band sections. A stopped section halts
// everything upstream while downstream runs empty (FC19); a wrong speed is
// corrected, or Maintenance is called (FC20). It receives section commands and a
// target speed (from MON), and publishes the state of the sections ("Band state
// change") to THM, FIN and MON, and transport faults as error events.
package bt

import (
	"fmt"
	"math/rand"
	"sync"
	"time"

	"cakefactory/internal/model"
	"cakefactory/internal/node"
	"cakefactory/internal/sig"
	"cakefactory/internal/sim"
	"cakefactory/internal/svc"
)

const (
	nominalSpeed = 50.0             // cm/s, the default target speed
	speedFixTime = 10 * time.Second // time the monitor gets to correct a wrong speed
	restartGap   = 1500 * time.Millisecond
)

// State codes of a section, also saved as gauges.
const (
	stRun = iota
	stSlow
	stHalt
	stFault
)

var stName = [...]string{"run", "slow", "halt", "fault"}

// BT is the band transport service.
type BT struct {
	*svc.Base
	mu     sync.Mutex
	saveMu sync.Mutex // orders the saves of the state
	cmd    [6]string  // run, slow or halt, as commanded by the monitor
	fault  [6]bool    // section stopped (FC19)
	hold   [6]bool    // halted because a section downstream stopped
	slow   [6]bool    // wrong speed (FC20)
	broken [6]bool    // wrong speed that could not be corrected: needs Maintenance
	target float64
}

// Run starts the service.
func Run() {
	b, cancel := svc.Run(model.BT, "ConveyorController", "ConveyorDrive", "SpeedSensor", "SectionStatusSensor")
	defer cancel()
	t := &BT{Base: b, target: nominalSpeed}
	for i := 1; i <= 5; i++ {
		t.cmd[i] = "run"
	}
	t.N.OnCommand(sig.SectionCommand, t.onSectionCommand)
	t.N.OnCommand(sig.TargetSpeed, func(s sig.Signal) {
		if v := s.Float("speed"); v > 0 {
			t.mu.Lock()
			t.target = v
			t.mu.Unlock()
			t.N.Log("FC20", fmt.Sprintf("target speed set to %.0f cm/s", v), "")
		}
	})
	for i := 1; i <= 5; i++ {
		i := i
		t.OnRepair(fmt.Sprintf("bt-section-%d", i), func() { t.repaired(i) })
	}
	t.Start()
	t.restore()
	t.publish()
	go t.faultLoop()
	go t.speedLoop()
	<-t.Ctx.Done()
}

// restore takes the stopped sections back from the saved state after a restart.
func (t *BT) restore() {
	t.mu.Lock()
	n := 0
	for i := 1; i <= 5; i++ {
		if t.N.GaugeValue(fmt.Sprintf("state%d", i)) == stFault {
			t.fault[i] = true
			for u := 1; u < i; u++ {
				t.hold[u] = true
			}
			n++
		}
	}
	t.mu.Unlock()
	if n > 0 {
		t.N.Log("FC27", fmt.Sprintf("restart: %d stopped band sections taken up again from the saved state", n), "")
	}
}

// haltedBehind reports whether a section downstream of i is halted by command: a stopped
// section stops everything upstream of it (the same rule as for a stopped section, FC19).
func (t *BT) haltedBehind(i int) bool {
	for j := i + 1; j <= 5; j++ {
		if t.cmd[j] == "halt" {
			return true
		}
	}
	return false
}

func (t *BT) effective(i int) int {
	switch {
	case t.fault[i]:
		return stFault
	case t.hold[i] || t.cmd[i] == "halt" || t.haltedBehind(i):
		return stHalt
	case t.slow[i] || t.cmd[i] == "slow":
		return stSlow
	}
	return stRun
}

// publish sends "Band state change" (S1..S5 for THM and FIN, D1..D5 with the
// detail "fault" for the monitor) and updates the devices and gauges.
func (t *BT) publish() {
	t.mu.Lock()
	kv := []any{}
	worst, anyFault, anySlow := stRun, false, false
	for i := 1; i <= 5; i++ {
		e := t.effective(i)
		show := e
		if e == stFault {
			show = stHalt // THM and FIN only need to know that the section does not run
			anyFault = true
		}
		if e == stSlow {
			anySlow = true
		}
		if e > worst {
			worst = e
		}
		kv = append(kv, fmt.Sprintf("S%d", i), stName[show], fmt.Sprintf("D%d", i), stName[e])
		t.N.Gauge(fmt.Sprintf("state%d", i), int64(e))
	}
	t.mu.Unlock()
	t.persist()
	t.N.SetState(sig.New(sig.BandState, kv...))
	switch {
	case anyFault:
		t.N.SetDevice("ConveyorDrive", model.StateFault, "a section stopped")
	case anySlow:
		t.N.SetDevice("ConveyorDrive", model.StateWarn, "wrong speed or slow-down")
	default:
		t.N.SetDevice("ConveyorDrive", model.StateOK, "")
	}
	if worst == stRun {
		t.N.SetDevice("SectionStatusSensor", model.StateOK, "")
	} else {
		t.N.SetDevice("SectionStatusSensor", model.StateWarn, "a section does not run")
	}
}

// persist saves the state of the sections at once. A stopped section must not come
// back from an old snapshot after a restart (it would stay stopped with no ticket
// to repair it), so this does not wait for the periodic snapshot of the service.
func (t *BT) persist() {
	t.saveMu.Lock()
	defer t.saveMu.Unlock()
	_ = t.Pans.SaveState(t.Ctx, model.BT, t.N.Gauges())
}

// onSectionCommand is the "Section command" port: the monitor halts, slows or
// restarts sections, either by number or all sections owned by a system.
func (t *BT) onSectionCommand(s sig.Signal) {
	state, reason := s.Str("state"), s.Str("reason")
	if state != "run" && state != "slow" && state != "halt" {
		return
	}
	var which []int
	for i := 1; i <= 5; i++ {
		switch {
		case s.Str("node") != "" && model.OwnsSection(s.Str("node"), i):
			which = append(which, i)
		case s.Str("node") == "" && (s.Int("section") == i || s.Int("section") == 0):
			which = append(which, i)
		}
	}
	changed := false
	t.mu.Lock()
	for _, i := range which {
		if t.cmd[i] != state {
			t.cmd[i] = state
			changed = true
		}
	}
	t.mu.Unlock()
	if !changed {
		return // the command is repeated when the monitor restarts; nothing to do
	}
	t.publish()
	switch state {
	case "halt":
		t.N.Warn("UC2", fmt.Sprintf("production halt reported to the overseer: sections %v halted (%s)", which, reason), "")
	case "slow":
		t.N.Warn("UC2", fmt.Sprintf("production slow-down reported to the overseer: sections %v slowed (%s)", which, reason), "")
	default:
		t.N.Log("UC2", fmt.Sprintf("sections %v run again (%s)", which, reason), "")
	}
}

// faultLoop turns injected failure cases into band faults.
func (t *BT) faultLoop() {
	tick := time.NewTicker(300 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-t.Ctx.Done():
			return
		case <-tick.C:
		}
		if t.F.Take("FC19") {
			t.stopSection(t.pick())
		}
		if t.F.Take("FC20") {
			fixable := !t.F.Take("FC20") // armed twice: the correction fails
			go t.wrongSpeed(t.pick(), fixable)
		}
	}
}

// pick chooses a section that is not already in trouble.
func (t *BT) pick() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	start := rand.Intn(5) + 1
	for k := 0; k < 5; k++ {
		i := (start-1+k)%5 + 1
		if !t.fault[i] && !t.slow[i] {
			return i
		}
	}
	return start
}

// stopSection is FC19: the drive of section i stops. Section Control halts every
// section upstream; downstream sections run until empty. Fault Handling reports
// the transport fault.
func (t *BT) stopSection(i int) {
	t.mu.Lock()
	t.fault[i] = true
	for u := 1; u < i; u++ {
		t.hold[u] = true
	}
	t.mu.Unlock()
	t.publish()
	t.N.Emit(nodeFault("FC19", fmt.Sprintf("band section %d (%s) stopped: upstream sections halted, downstream run until empty", i, model.SectionName(i)),
		"ConveyorDrive", fmt.Sprintf("bt-section-%d", i)))
}

// wrongSpeed is FC20, "System, then Maintenance": the monitor first tries to
// correct the speed; when that fails Maintenance repairs the drive.
func (t *BT) wrongSpeed(i int, fixable bool) {
	t.mu.Lock()
	t.slow[i] = true
	target := t.target
	t.mu.Unlock()
	t.publish()
	t.N.SetDevice("SpeedSensor", model.StateWarn, fmt.Sprintf("section %d: %.0f cm/s, target %.0f", i, 0.6*target, target))
	t.N.Warn("FC20", fmt.Sprintf("section %d (%s) runs at %.0f cm/s, target %.0f cm/s: speed correction requested", i, model.SectionName(i), 0.6*target, target), "")
	if !sim.Sleep(t.Ctx, speedFixTime) {
		return
	}
	if fixable {
		t.mu.Lock()
		t.slow[i] = false
		t.mu.Unlock()
		t.publish()
		t.N.SetDevice("SpeedSensor", model.StateOK, "")
		t.N.Log("FC20", fmt.Sprintf("section %d speed corrected", i), "")
		return
	}
	t.mu.Lock()
	t.broken[i] = true
	t.mu.Unlock()
	t.N.SetDevice("SpeedSensor", model.StateFault, "speed cannot be corrected")
	t.N.Emit(nodeFault("FC20", fmt.Sprintf("section %d speed could not be corrected: Maintenance repairs the band drive", i),
		"ConveyorDrive", fmt.Sprintf("bt-section-%d", i)))
}

// repaired is "Repair confirmed" for a section: it runs again and the sections
// upstream restart one by one, in order.
func (t *BT) repaired(i int) {
	t.mu.Lock()
	was := t.fault[i] || t.broken[i]
	t.fault[i], t.broken[i], t.slow[i] = false, false, false
	t.mu.Unlock()
	t.N.SetDevice("SpeedSensor", model.StateOK, "")
	t.publish()
	if !was {
		return
	}
	t.N.Log("FC19", fmt.Sprintf("section %d repaired and running; sections upstream restart in order", i), "")
	go func() {
		for u := i - 1; u >= 1; u-- {
			if !sim.Sleep(t.Ctx, restartGap) {
				return
			}
			t.mu.Lock()
			t.hold[u] = false
			t.mu.Unlock()
			t.publish()
		}
		t.N.Log("FC19", "all band sections running again", "")
	}()
}

// speedLoop is Speed Monitoring: the speed sensor reading of every section.
func (t *BT) speedLoop() {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-t.Ctx.Done():
			return
		case <-tick.C:
		}
		t.mu.Lock()
		target := t.target
		var v [6]float64
		for i := 1; i <= 5; i++ {
			switch t.effective(i) {
			case stRun:
				v[i] = target * (1 + sim.Noise(0.01))
			case stSlow:
				if t.slow[i] {
					v[i] = 0.6 * target
				} else {
					v[i] = 0.5 * target
				}
			}
		}
		t.mu.Unlock()
		t.N.Gauge("target", int64(target))
		for i := 1; i <= 5; i++ {
			t.N.Gauge(fmt.Sprintf("speed%d", i), int64(v[i]+0.5))
		}
	}
}

// nodeFault builds the error event that reports a transport fault to the monitor
// and asks for Maintenance (the "transportFault" port).
func nodeFault(code, reason, device, machine string) node.Event {
	return node.Event{Severity: model.SevError, Code: code, Reason: reason, Device: device,
		MachineID: machine, Maintenance: true}
}
