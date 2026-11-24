package plm

import (
	"fmt"
	"log"
	"sync"
	"time"

	"cakefactory/internal/model"
	"cakefactory/internal/sig"
	"cakefactory/internal/store"
	"cakefactory/internal/svc"
)

// RunShip starts the shipment station: it assigns finished pallets to vehicles.
func RunShip() {
	b, cancel := svc.Run(model.PLMShip, "TransportPlanner")
	defer cancel()
	sqlStore, err := store.OpenSQL(b.Ctx)
	if err != nil {
		log.Fatal(err)
	}
	var mu sync.Mutex
	palletNo := 0
	pallets := make(chan sig.Signal, 1024)
	b.N.OnSignal(model.FINPal, sig.PalletReady, func(_ string, s sig.Signal) { pallets <- s })
	b.Start()
	vehicles := []string{"Truck 1", "Truck 2", "Truck 3"}
	for {
		select {
		case <-b.Ctx.Done():
			return
		case s := <-pallets:
			mu.Lock()
			palletNo++
			v := vehicles[palletNo%len(vehicles)]
			mu.Unlock()
			cakes := s.Int("cakes")
			sh := model.Shipment{PalletID: s.Str("palletId"), Cakes: cakes, Batches: s.Str("batches"), Vehicle: v, At: time.Now().UTC()}
			if err := sqlStore.SaveShipment(b.Ctx, sh); err != nil {
				log.Printf("save shipment: %v", err)
			}
			b.N.AddGauge("pallets.shipped", 1)
			b.N.AddGauge("cakes.shipped", int64(cakes))
			b.N.Log("UC16", fmt.Sprintf("pallet %s (%d cakes) assigned to %s and dispatched", sh.PalletID, cakes, v), s.Str("batches"))
		}
	}
}
