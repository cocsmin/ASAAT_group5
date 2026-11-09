// Package plm is the Production & Logistics Management system, split in two
// containers: PLM-WH (orders and scheduling, recipes, warehouse and inventory in
// SQL) and PLM-SHIP (transport planning and shipment, ship.go).
//
// PLM-WH is the state machine of the Papyrus model (machine.go): one goroutine handles
// the events one at a time, in the state the machine is in.
package plm

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"cakefactory/internal/model"
	"cakefactory/internal/sig"
	"cakefactory/internal/sim"
	"cakefactory/internal/store"
	"cakefactory/internal/svc"
)

// PLM is the Production & Logistics Management service of the container PLM-WH.
type PLM struct {
	*svc.Base
	sql    store.SQL
	m      *machine
	events chan event
}

// nodeHost connects the state machine to the Sparkplug node, the fault registry and simulated time.
type nodeHost struct{ p *PLM }

func (h nodeHost) Send(s sig.Signal)                   { h.p.N.SendReliable(s) }
func (h nodeHost) Log(code, text, batch string)        { h.p.N.Log(code, text, batch) }
func (h nodeHost) Warn(code, text, batch string)       { h.p.N.Warn(code, text, batch) }
func (h nodeHost) Device(name, state, detail string)   { h.p.N.SetDevice(name, state, detail) }
func (h nodeHost) Gauge(name string, v int64)          { h.p.N.Gauge(name, v) }
func (h nodeHost) Fault(code string) bool              { return h.p.F.Take(code) }
func (h nodeHost) Online() bool                        { return h.p.N.HostOnline() }
func (h nodeHost) Wait(d time.Duration) bool           { return sim.Sleep(h.p.Ctx, d) }
func (h nodeHost) Now() time.Time                      { return time.Now() }
func (h nodeHost) Scale(d time.Duration) time.Duration { return sim.Scale(d) }
func (h nodeHost) Later(d time.Duration, ev event) {
	time.AfterFunc(sim.Scale(d), func() { h.p.post(ev) })
}

func (p *PLM) post(ev event) {
	select {
	case p.events <- ev:
	case <-p.Ctx.Done():
	}
}

// RunWh starts the orders and warehouse station.
func RunWh() {
	b, cancel := svc.Run(model.PLMWh, "OrderScheduler", "RecipeManager", "WarehouseInventory")
	defer cancel()
	sqlStore, err := store.OpenSQL(b.Ctx)
	if err != nil {
		log.Fatal(err)
	}
	p := &PLM{Base: b, sql: sqlStore, events: make(chan event, 1024)}
	p.m = newMachine(nodeHost{p}, sqlStore, b.Ctx)
	p.N.OnSignal(model.BATDose, sig.IngredientReq, func(_ string, s sig.Signal) {
		if s.Bool("request") {
			p.post(event{kind: evRequest, req: s})
		}
	})
	p.Start()
	// Continue the order numbers after a restart. Taking the larger of the saved
	// counter and the highest stored order avoids re-issuing an id that batter
	// preparation already knows (it would ignore the order and the line would stall).
	if n, err := sqlStore.LastOrderNo(b.Ctx); err == nil {
		p.m.orderNo = n
	}
	if g := int(p.N.GaugeValue("orders.created")); g > p.m.orderNo {
		p.m.orderNo = g
	}
	go p.stockLoop()
	go p.timers(svc.EnvDur("ORDER_SECONDS", 22*time.Second))
	for {
		select {
		case <-p.Ctx.Done():
			return
		case ev := <-p.events:
			p.m.handle(ev)
		}
	}
}

// timers posts the order cycle (the first order at once, when the monitor is online) and the
// check for orders that batter preparation did not pick up.
func (p *PLM) timers(every time.Duration) {
	for !p.N.HostOnline() { // production may not run without the monitor
		if !sim.Sleep(p.Ctx, time.Second) {
			return
		}
	}
	p.post(event{kind: evOrderTimer})
	order := time.NewTicker(sim.Scale(every))
	defer order.Stop()
	resend := time.NewTicker(sim.Scale(10 * time.Second))
	defer resend.Stop()
	for {
		select {
		case <-p.Ctx.Done():
			return
		case <-order.C:
			p.post(event{kind: evOrderTimer})
		case <-resend.C:
			p.post(event{kind: evResend})
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
