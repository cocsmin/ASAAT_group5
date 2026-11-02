// Package plm is the Production & Logistics Management system: orders and
// scheduling, recipes, warehouse and inventory (SQL), transport and shipment.
package plm

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"cakefactory/internal/model"
	"cakefactory/internal/sig"
	"cakefactory/internal/sim"
	"cakefactory/internal/store"
	"cakefactory/internal/svc"
)

// Timing, in simulated time.
const (
	warehouseTimeout = 2 * time.Second  // request timeout toward the warehouse
	breakerWindow    = 15 * time.Second // circuit breaker stays open this long
	restockDelay     = 15 * time.Second // Logistics restocks after being notified
)

// PLM is the Production & Logistics Management service.
type PLM struct {
	*svc.Base
	sql store.SQL

	mu        sync.Mutex
	orderNo   int
	palletNo  int
	sentOrder map[string]pendingOrder // orders sent, waiting for BAT's ingredient request
	held      map[string]model.Order
	reqs      chan sig.Signal
	pallets   chan sig.Signal
	breakerAt time.Time // breaker open until this wall-clock time
	probing   bool
}

// pendingOrder is an order that batter preparation has not yet picked up.
type pendingOrder struct {
	sig sig.Signal
	at  time.Time
}

// resendLoop sends an order again when batter preparation has not asked for its
// ingredients within 40 s. Batter preparation may have restarted and lost the
// order it had only received; it ignores an order it already knows.
func (p *PLM) resendLoop() {
	t := time.NewTicker(sim.Scale(10 * time.Second))
	defer t.Stop()
	for {
		select {
		case <-p.Ctx.Done():
			return
		case <-t.C:
		}
		var again []pendingOrder
		p.mu.Lock()
		for id, po := range p.sentOrder {
			if time.Since(po.at) > sim.Scale(40*time.Second) {
				po.at = time.Now()
				p.sentOrder[id] = po
				again = append(again, po)
			}
		}
		p.mu.Unlock()
		for _, po := range again {
			p.N.Warn("UC1", "order "+po.sig.Str("orderId")+" was not picked up by batter preparation: sent again", po.sig.Str("batchId"))
			p.N.SendReliable(po.sig)
		}
	}
}

// Run starts the service.
func Run() {
	b, cancel := svc.Run(model.PLM, "OrderScheduler", "RecipeManager", "WarehouseInventory", "TransportPlanner")
	defer cancel()
	sqlStore, err := store.OpenSQL(b.Ctx)
	if err != nil {
		log.Fatal(err)
	}
	p := &PLM{Base: b, sql: sqlStore, sentOrder: map[string]pendingOrder{}, held: map[string]model.Order{},
		reqs: make(chan sig.Signal, 1024), pallets: make(chan sig.Signal, 1024)}
	p.orderNo = int(p.N.GaugeValue("orders.created"))
	p.N.OnSignal(model.BAT, sig.IngredientReq, func(_ string, s sig.Signal) {
		if s.Bool("request") {
			p.reqs <- s
		}
	})
	p.N.OnSignal(model.FIN, sig.PalletReady, func(_ string, s sig.Signal) { p.pallets <- s })
	p.Start()
	go p.requestLoop()
	go p.resendLoop()
	go p.palletLoop()
	go p.stockLoop()
	p.orderLoop(svc.EnvDur("ORDER_SECONDS", 22*time.Second))
}

func (p *PLM) orderLoop(every time.Duration) {
	t := time.NewTicker(sim.Scale(every))
	defer t.Stop()
	for !p.N.HostOnline() { // production may not run without the monitor
		if !sim.Sleep(p.Ctx, time.Second) {
			return
		}
	}
	p.newOrder() // first order immediately
	for {
		select {
		case <-p.Ctx.Done():
			return
		case <-t.C:
			p.mu.Lock()
			outstanding, held := len(p.sentOrder), len(p.held)
			p.mu.Unlock()
			if outstanding >= 2 || held > 0 || !p.N.HostOnline() {
				continue // BAT is busy or an order is held for stock
			}
			p.newOrder()
		}
	}
}

func encodeItems(items []model.Item) string {
	parts := make([]string, len(items))
	for i, it := range items {
		parts[i] = fmt.Sprintf("%s:%g", it.Ingredient, it.Grams)
	}
	return strings.Join(parts, ";")
}

// DecodeItems parses the ingredient list carried in order signals.
func DecodeItems(s string) []model.Item {
	var out []model.Item
	for _, part := range strings.Split(s, ";") {
		name, g, ok := strings.Cut(part, ":")
		if !ok {
			continue
		}
		f, _ := strconv.ParseFloat(g, 64)
		out = append(out, model.Item{Ingredient: name, Grams: f})
	}
	return out
}

func (p *PLM) newOrder() {
	rec, err := p.sql.Recipe(p.Ctx, store.SeedRecipe.Name)
	if err != nil {
		p.N.Warn("PLM", "cannot read recipe: "+err.Error(), "")
		return
	}
	p.mu.Lock()
	p.orderNo++
	id := fmt.Sprintf("PO-%04d", p.orderNo)
	p.mu.Unlock()
	p.N.Gauge("orders.created", int64(p.orderNo))
	o := model.Order{ID: id, Recipe: rec.Name, Status: "released", Created: time.Now().UTC()}
	if err := p.sql.SaveOrder(p.Ctx, o); err != nil {
		log.Printf("save order: %v", err)
	}
	batch := "B" + strings.TrimPrefix(id, "PO-") // batch ID is created here, see ADR
	order := sig.New(sig.OrderAndRecipe,
		"orderId", id, "batchId", batch, "recipe", rec.Name, "items", encodeItems(rec.Items),
		"mixSeconds", int64(rec.MixSeconds), "doseTolPct", rec.DoseTolPct, "pansPerBatch", int64(rec.PansPerBatch),
		"fillTarget", rec.FillTarget, "fillTol", rec.FillTol)
	p.mu.Lock()
	p.sentOrder[id] = pendingOrder{sig: order, at: time.Now()}
	p.mu.Unlock()
	p.N.SendReliable(order)
	p.N.Log("UC1", "production order "+id+" released to batter preparation", batch)
}

func (p *PLM) requestLoop() {
	for {
		select {
		case <-p.Ctx.Done():
			return
		case s := <-p.reqs:
			p.handleRequest(s)
		}
	}
}

func (p *PLM) reply(orderID, batch, result, reason string) {
	p.N.SendReliable(sig.New(sig.IngredientReq, "request", false, "orderId", orderID, "batchId", batch,
		"result", result, "reason", reason))
}

func (p *PLM) handleRequest(s sig.Signal) {
	id, batch := s.Str("orderId"), s.Str("batchId")
	items := DecodeItems(s.Str("items"))
	p.mu.Lock()
	delete(p.sentOrder, id)
	open := time.Now().Before(p.breakerAt)
	p.mu.Unlock()

	// FC2: circuit breaker toward the warehouse.
	if open {
		p.N.SetDevice("WarehouseInventory", model.StateWarn, "circuit breaker open")
		p.reply(id, batch, "staged", "warehouse unavailable: continue with staged ingredients")
		p.N.Warn("FC2", "circuit breaker open: batching continues with staged ingredients", batch)
		p.scheduleProbe()
		return
	}
	if p.F.Take("FC2") {
		sim.Sleep(p.Ctx, warehouseTimeout)
		p.mu.Lock()
		p.breakerAt = time.Now().Add(sim.Scale(breakerWindow))
		p.mu.Unlock()
		p.N.Gauge("breaker", 1)
		p.N.SetDevice("WarehouseInventory", model.StateWarn, "warehouse timed out, breaker open")
		p.N.Warn("FC2", "warehouse request timed out: circuit breaker opened", batch)
		p.reply(id, batch, "staged", "warehouse unavailable: continue with staged ingredients")
		p.scheduleProbe()
		return
	}
	if p.F.Take("FC1") {
		p.drainStock("eggs") // an injected shortage: the real reservation then fails
	}
	p.mu.Lock()
	_, held := p.held[id]
	p.mu.Unlock()
	p.reserve(id, batch, items, !held) // a repeated request for a held order is answered quietly
}

// drainStock leaves very little of an ingredient in stock.
func (p *PLM) drainStock(ing string) {
	rows, err := p.sql.Stock(p.Ctx)
	if err != nil {
		return
	}
	for _, r := range rows {
		if r.Ingredient == ing && r.Grams-r.Reserved > 50 {
			_ = p.sql.Restock(p.Ctx, ing, -(r.Grams - r.Reserved - 50))
		}
	}
}

func (p *PLM) reserve(id, batch string, items []model.Item, first bool) {
	ok, reason, err := p.sql.Reserve(p.Ctx, id, items)
	if err != nil {
		p.N.Warn("PLM", "reservation error: "+err.Error(), batch)
		p.reply(id, batch, "staged", "warehouse error")
		return
	}
	if ok {
		p.mu.Lock()
		delete(p.held, id)
		held := len(p.held)
		p.mu.Unlock()
		p.N.Gauge("orders.held", int64(held))
		p.N.SetDevice("WarehouseInventory", model.StateOK, "")
		_ = p.sql.SetOrderStatus(p.Ctx, id, "reserved", "")
		p.reply(id, batch, "confirmed", "")
		if !first {
			p.N.Log("FC1", "stock restored: held order "+id+" released", batch)
		}
		go func() { // ingredients are collected from the warehouse shortly after
			if sim.Sleep(p.Ctx, 3*time.Second) {
				_ = p.sql.Consume(p.Ctx, id)
			}
		}()
		return
	}
	// FC1: not enough ingredients. Hold the order, notify Logistics, restock later.
	p.mu.Lock()
	p.held[id] = model.Order{ID: id}
	p.mu.Unlock()
	p.N.Gauge("orders.held", int64(len(p.held)))
	_ = p.sql.SetOrderStatus(p.Ctx, id, "held", reason)
	p.N.SetDevice("WarehouseInventory", model.StateWarn, reason)
	p.reply(id, batch, "insufficient", reason)
	if first {
		p.N.Warn("FC1", reason+": order "+id+" held, Logistics notified", batch)
		go p.restock(id, batch, items, reason)
	}
}

func (p *PLM) restock(id, batch string, items []model.Item, reason string) {
	if !sim.Sleep(p.Ctx, restockDelay) {
		return
	}
	for _, it := range items {
		_ = p.sql.Restock(p.Ctx, it.Ingredient, 2*it.Grams+5000)
	}
	p.N.Log("FC1", "Logistics restocked ingredients ("+reason+")", batch)
	p.reserve(id, batch, items, false)
}

// scheduleProbe sends the single test request that closes the breaker.
func (p *PLM) scheduleProbe() {
	p.mu.Lock()
	if p.probing {
		p.mu.Unlock()
		return
	}
	p.probing = true
	wait := time.Until(p.breakerAt)
	p.mu.Unlock()
	go func() {
		select {
		case <-p.Ctx.Done():
			return
		case <-time.After(wait):
		}
		_, err := p.sql.Stock(p.Ctx) // test request
		p.mu.Lock()
		p.probing = false
		if err == nil {
			p.breakerAt = time.Time{}
		}
		p.mu.Unlock()
		if err == nil {
			p.N.Gauge("breaker", 0)
			p.N.SetDevice("WarehouseInventory", model.StateOK, "")
			p.N.Log("FC2", "test request succeeded: circuit breaker closed", "")
		}
	}()
}

func (p *PLM) palletLoop() {
	vehicles := []string{"Truck 1", "Truck 2", "Truck 3"}
	for {
		select {
		case <-p.Ctx.Done():
			return
		case s := <-p.pallets:
			p.mu.Lock()
			p.palletNo++
			v := vehicles[p.palletNo%len(vehicles)]
			p.mu.Unlock()
			cakes := s.Int("cakes")
			sh := model.Shipment{PalletID: s.Str("palletId"), Cakes: cakes, Batches: s.Str("batches"), Vehicle: v, At: time.Now().UTC()}
			if err := p.sql.SaveShipment(p.Ctx, sh); err != nil {
				log.Printf("save shipment: %v", err)
			}
			p.N.AddGauge("pallets.shipped", 1)
			p.N.AddGauge("cakes.shipped", int64(cakes))
			p.N.Log("UC16", fmt.Sprintf("pallet %s (%d cakes) assigned to %s and dispatched", sh.PalletID, cakes, v), s.Str("batches"))
		}
	}
}

func (p *PLM) stockLoop() {
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-p.Ctx.Done():
			return
		case <-t.C:
			rows, err := p.sql.Stock(context.Background())
			if err != nil {
				continue
			}
			for _, r := range rows {
				p.N.Gauge("stock."+r.Ingredient, int64(r.Grams-r.Reserved))
			}
		}
	}
}
