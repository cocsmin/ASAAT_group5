package fin

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"cakefactory/internal/model"
	"cakefactory/internal/sig"
	"cakefactory/internal/station"
	"cakefactory/internal/store"
	"cakefactory/internal/svc"
)

// Pal is the pallet station: it cartons, checks the case weight and stacks cakes on
// pallets (UC16, FC26).
type Pal struct {
	*svc.Base
	sec        *station.Section
	took       *station.PanSet
	palletSize int
	toPallet   chan cake

	mu       sync.Mutex
	palletNo int
}

// RunPal starts the pallet station.
func RunPal() {
	b, cancel := svc.Run(model.FINPal, "Cartoner", "CaseCheckweigher", "PalletPacker")
	defer cancel()
	size, _ := strconv.Atoi(svc.Env("PALLET_SIZE", "30"))
	f := &Pal{Base: b, sec: station.NewSection(5), took: station.NewPanSet(), palletSize: size, toPallet: make(chan cake, 1024)}
	f.N.OnSignal(model.FINWrap, sig.CakeToCartoning, func(_ string, s sig.Signal) {
		f.enqueue(cake{pan: s.Str("panId"), batch: s.Str("batchId")})
	})
	f.N.OnSignal(model.BT, sig.BandState, func(_ string, s sig.Signal) { f.sec.OnBand(s) })
	f.Start()
	station.Reconcile(f.Base, f.adopt)
	f.palletiser()
}

func (f *Pal) enqueue(c cake) {
	if f.took.Add(c.doc()) {
		f.toPallet <- c
	}
}

// adopt takes up the wrapped cakes that arrived without a message.
func (f *Pal) adopt() {
	all, err := f.Pans.List(f.Ctx, 1000)
	if err != nil {
		return
	}
	n := 0
	for i := len(all) - 1; i >= 0; i-- {
		p := all[i]
		if strings.HasPrefix(p.ID, "CAKE-") && p.Status == "wrapped" && p.Station == "FIN-Wrapping" && time.Since(p.Updated) > station.HandoverGrace {
			c := cake{pan: p.PanID, batch: p.BatchID}
			if f.took.Add(c.doc()) {
				f.toPallet <- c
				n++
			}
		}
	}
	if n > 0 {
		log.Printf("[%s] %d cakes taken up from the stored documents", f.ID, n)
	}
}

func (f *Pal) palletiser() {
	var batches []string
	count := 0
	for {
		var c cake
		select {
		case <-f.Ctx.Done():
			return
		case c = <-f.toPallet:
		}
		if !f.sec.Span(f.Base, cartonTime) {
			return
		}
		if f.F.Take("FC26") {
			f.N.SetDevice("CaseCheckweigher", model.StateWarn, "wrong case weight")
			store.Move(f.Ctx, f.Pans, c.doc(), "FIN-Cartoning", "set aside", "wrong case weight", nil)
			f.N.Warn("FC26", "wrong case weight: case set aside for manual check", c.batch)
			f.took.Drop(c.doc())
			continue
		}
		f.N.SetDevice("CaseCheckweigher", model.StateOK, "")
		store.Move(f.Ctx, f.Pans, c.doc(), "FIN-Pallet", "packed", "", nil)
		f.took.Drop(c.doc())
		f.N.AddGauge("packed", 1)
		count++
		if !strings.Contains(strings.Join(batches, ","), c.batch) {
			batches = append(batches, c.batch)
		}
		f.N.Gauge("palletFill", int64(count))
		if count < f.palletSize {
			continue
		}
		f.sec.Span(f.Base, wrapTime) // wrap the pallet
		f.mu.Lock()
		f.palletNo++
		id := fmt.Sprintf("PAL-%04d", f.palletNo)
		f.mu.Unlock()
		f.N.AddGauge("pallets", 1)
		f.N.SendReliable(sig.New(sig.PalletReady, "palletId", id, "cakes", int64(count), "batches", strings.Join(batches, ",")))
		f.N.Log("UC16", fmt.Sprintf("pallet %s full (%d cakes), wrapped and ready for shipment", id, count), strings.Join(batches, ","))
		count, batches = 0, nil
		f.N.Gauge("palletFill", 0)
	}
}
