package svc

import (
	"log"
	"math/rand"
	"os"
	"strconv"
	"time"

	"cakefactory/internal/model"
	"cakefactory/internal/sig"
	"cakefactory/internal/sim"
)

// chaos makes the failure cases of this container happen by themselves, at random, with the
// rates of the fault catalog (PerHour) multiplied by FAULT_RATE. FAULT_RATE=0 (the default)
// is a factory that only fails when someone injects a fault; 1 is the rates of the catalog;
// 5 is five times as many failures. The failures are random: there is no schedule.
func (b *Base) chaos() {
	mult, err := strconv.ParseFloat(os.Getenv("FAULT_RATE"), 64)
	if err != nil || mult <= 0 {
		return
	}
	speed := sim.Unscale(time.Second).Seconds()
	log.Printf("[%s] random failures on: rates of the catalog times %.2f (simulation speed %.1f)", b.ID, mult, speed)
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-b.Ctx.Done():
			return
		case <-t.C:
		}
		for _, f := range model.ChaosDue(b.ID, mult, speed, time.Second, rand.Float64) {
			log.Printf("[%s] random failure %s: %s", b.ID, f.Code, f.Title)
			b.onInject(sig.New(sig.FaultInjection, "code", f.Code, "count", int64(f.Count)))
		}
	}
}
