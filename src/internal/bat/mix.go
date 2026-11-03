package bat

import (
	"fmt"
	"time"

	"cakefactory/internal/model"
	"cakefactory/internal/sig"
	"cakefactory/internal/sim"
	"cakefactory/internal/svc"
)

// Mix is the mixing station: it mixes a dosed batch with consistency monitoring
// (UC4, FC6) and hands the batter to the inspection gate. When the inspector
// rejects the batter it mixes it again.
type Mix struct {
	*svc.Base
	jobs chan job
}

// RunMix starts the mixing station.
func RunMix() {
	b, cancel := svc.Run(model.BATMix, "Mixer", "ConsistencySensor")
	defer cancel()
	t := &Mix{Base: b, jobs: make(chan job, 256)}
	t.N.OnSignal(model.BATDose, sig.BatchToMix, func(_ string, s sig.Signal) { t.jobs <- jobOf(s) })
	t.N.OnSignal(model.BATInsp, sig.BatterRejected, func(_ string, s sig.Signal) { // another mixing cycle starts immediately
		t.N.Warn("UC4", "inspector marked batter not good: another mixing cycle starts immediately", s.Str("batchId"))
		t.jobs <- jobOf(s)
	})
	t.Start()
	t.N.Gauge(stageMixing, 0)
	for {
		select {
		case <-t.Ctx.Done():
			return
		case j := <-t.jobs:
			t.mix(j)
		}
	}
}

// mix runs one mixing cycle and passes the batter on, or discards the batch when
// the mixer breaks.
func (t *Mix) mix(j job) {
	t.N.Gauge(stageMixing, 1)
	defer t.N.Gauge(stageMixing, 0)
	t.N.SetDevice("Mixer", model.StateOK, "mixing")
	defer func() {
		if !t.F.Broken(mixerMachine) {
			t.N.SetDevice("Mixer", model.StateOK, "")
		}
	}()
	step := time.Duration(j.mixSeconds) * time.Second / 6
	for i := 0; i < 6; i++ {
		if !sim.Sleep(t.Ctx, step) {
			return
		}
		if t.F.Take("FC6") {
			t.F.Break(mixerMachine)
			t.N.SetDevice("Mixer", model.StateFault, "mixer broke")
			t.N.Discard("FC6", "mixer broke: batch in the mixer discarded", j.batch)
			t.N.SendReliable(sig.New(sig.BatchResult, "batchId", j.batch, "result", "discarded"))
			t.N.Fault("FC6", "mixer broke during the mixing cycle", j.batch, "Mixer", mixerMachine)
			if !t.WaitRepaired(mixerMachine) {
				return
			}
			t.N.Log("FC6", "mixer repaired: mixing resumes", j.batch)
			return
		}
		t.N.SetDevice("ConsistencySensor", model.StateOK, fmt.Sprintf("viscosity %.1f", 60+float64(i)*3+sim.Noise(0.4)))
	}
	t.N.SendReliable(sig.New(sig.BatterToInspect, j.fields()...))
}
