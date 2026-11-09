package plm

import (
	"context"
	"fmt"
	"time"

	"cakefactory/internal/model"
	"cakefactory/internal/sig"
	"cakefactory/internal/store"
	"cakefactory/internal/timing"
)

// Timing, in simulated time.
const (
	warehouseTimeout = timing.WarehouseTimeout // a request to the warehouse that takes longer is a wms_timeout
	breakerWindow    = timing.BreakerOpen      // Communication_Suspended: test the warehouse again after this
	restockDelay     = 15 * time.Second        // Logistics restocks after being notified (Restock signal)
	withdrawalTime   = 3 * time.Second         // the ingredients are collected from the warehouse (Withdrawal confirmation)
	withdrawalRetry  = 5 * time.Second         // book the withdrawal again when the database did not answer
	maxOutstanding   = 2                       // orders sent that batter preparation has not yet asked ingredients for
)

const stagedReason = "warehouse unavailable: continue with staged ingredients"

// State is a state of the PLM state machine (Papyrus: PLM, StateMachine22). The names are the
// ones of the diagram; the diagram spells Verifying_Inventory "Veryfing_Inventory".
type State string

// The states. Only Idle, Waiting_For_Withdrawal, Holding_Order and Communication_Suspended are
// resting states; the others are left again within the same step.
const (
	Idle                   State = "Idle"
	SchedulingOrder        State = "Scheduling_Order"
	FetchingRecipeTargets  State = "Fetching_recipe_targets"
	VerifyingInventory     State = "Verifying_Inventory"
	ReservingIngredients   State = "Reserving_Ingredients"
	WaitingForWithdrawal   State = "Waiting_For_Withdrawal"
	UpdatingInventory      State = "Updating_Inventory"
	HoldingOrder           State = "Holding_Order"
	CommunicationSuspended State = "Communication_Suspended"
)

// stateNo is the value of the gauge plm.state.
var stateNo = map[State]int64{
	Idle: 0, SchedulingOrder: 1, FetchingRecipeTargets: 2, VerifyingInventory: 3, ReservingIngredients: 4,
	WaitingForWithdrawal: 5, UpdatingInventory: 6, HoldingOrder: 7, CommunicationSuspended: 8,
}

type evKind int

const (
	evOrderTimer evKind = iota // the order cycle ran out
	evRequest                  // "Ingredient request" from batter preparation
	evWithdrawn                // "Withdrawal confirmation" (internal: the ingredients were collected)
	evRestock                  // "Restock signal" (internal: Logistics restocked)
	evProbe                    // time to test the warehouse again (leads to wms_responsive_confirmation)
	evResend                   // look for orders that batter preparation did not pick up
)

// event is one input of the state machine. All events are handled one at a time.
type event struct {
	kind  evKind
	req   sig.Signal // evRequest
	order string     // evWithdrawn, evRestock
}

// host is what the state machine needs from the outside: signals, events, gauges, faults, time.
type host interface {
	Send(sig.Signal)
	Log(code, text, batch string)
	Warn(code, text, batch string)
	Device(name, state, detail string)
	Gauge(name string, v int64)
	Fault(code string) bool              // fault injection: consumes one charge
	Online() bool                        // the monitor is online (no production without it)
	Wait(d time.Duration) bool           // wait a simulated time; false when the station stops
	Later(d time.Duration, ev event)     // post ev after a simulated time
	Now() time.Time                      // wall-clock time
	Scale(d time.Duration) time.Duration // simulated to wall-clock time
}

type pendingOrder struct {
	sig sig.Signal
	at  time.Time
}

// active is the order the machine is working on.
type active struct {
	id, batch string
	items     []model.Item
	held      bool   // it has been held for stock at least once
	reason    string // why it is held
}

// machine is the PLM state machine. It is used by one goroutine only.
type machine struct {
	h       host
	sql     store.SQL
	ctx     context.Context
	state   State
	orderNo int
	sent    map[string]pendingOrder // orders sent, waiting for the ingredient request of BAT
	cur     *active
	pending []sig.Signal // ingredient requests that came while the machine was busy with another order
	due     bool         // the order cycle ran out while the machine was busy
}

func newMachine(h host, sql store.SQL, ctx context.Context) *machine {
	m := &machine{h: h, sql: sql, ctx: ctx, sent: map[string]pendingOrder{}}
	m.enter(Idle)
	return m
}

func (m *machine) enter(s State) {
	m.state = s
	m.h.Gauge("plm.state", stateNo[s])
}

// handle runs one transition of the state machine.
func (m *machine) handle(ev event) {
	switch ev.kind {
	case evOrderTimer:
		if m.state == Idle {
			m.schedule()
		} else {
			m.due = true // taken up when the machine is Idle again
		}
	case evRequest:
		delete(m.sent, ev.req.Str("orderId")) // batter preparation has the order
		m.request(ev.req)
	case evWithdrawn:
		m.withdrawn(ev.order)
	case evRestock:
		m.restocked(ev.order)
	case evProbe:
		m.probe()
	case evResend:
		m.resend()
	}
}

// idle is the state Idle: set the system state to ready, then take up what waited meanwhile.
func (m *machine) idle() {
	m.cur = nil
	m.enter(Idle)
	if len(m.pending) > 0 {
		s := m.pending[0]
		m.pending = m.pending[1:]
		m.verify(s)
		return
	}
	if m.due {
		m.due = false
		m.schedule()
	}
}

// schedule is Idle to Scheduling_Order to Fetching_recipe_targets and back to Idle: a new
// production order is created and sent to BAT. It is not created while BAT is busy with two
// orders or while the monitor is offline (no production without the monitor).
func (m *machine) schedule() {
	if len(m.sent) >= maxOutstanding || !m.h.Online() {
		return
	}
	m.enter(SchedulingOrder)
	m.orderNo++
	id := fmt.Sprintf("PO-%04d", m.orderNo)
	m.h.Gauge("orders.created", int64(m.orderNo))
	_ = m.sql.SaveOrder(m.ctx, model.Order{ID: id, Recipe: store.SeedRecipe.Name, Status: "pending", Created: m.h.Now().UTC()})

	m.enter(FetchingRecipeTargets)
	var rec model.Recipe
	err := m.call(func(ctx context.Context) error {
		var e error
		rec, e = m.sql.Recipe(ctx, store.SeedRecipe.Name)
		return e
	})
	if err != nil {
		m.h.Warn("PLM", "cannot read recipe: "+err.Error(), "")
		_ = m.sql.SetOrderStatus(m.ctx, id, "cancelled", "recipe not available")
		m.idle()
		return
	}
	batch := "B" + id[len("PO-"):] // the batch id is created here, see ADR
	order := sig.New(sig.OrderAndRecipe,
		"orderId", id, "batchId", batch, "recipe", rec.Name, "items", encodeItems(rec.Items),
		"mixSeconds", int64(rec.MixSeconds), "doseTolPct", rec.DoseTolPct, "pansPerBatch", int64(rec.PansPerBatch),
		"fillTarget", rec.FillTarget, "fillTol", rec.FillTol)
	m.sent[id] = pendingOrder{sig: order, at: m.h.Now()}
	m.h.Send(order)
	_ = m.sql.SetOrderStatus(m.ctx, id, "released", "")
	m.h.Log("UC1", "production order "+id+" released to batter preparation", batch)
	m.idle()
}

// request handles an "Ingredient request" according to the state the machine is in.
func (m *machine) request(s sig.Signal) {
	id, batch := s.Str("orderId"), s.Str("batchId")
	switch m.state {
	case Idle:
		m.verify(s)
	case WaitingForWithdrawal:
		if m.cur != nil && m.cur.id == id {
			return // the answer is on its way
		}
		m.pending = append(m.pending, s)
	case HoldingOrder:
		if m.cur != nil && m.cur.id == id {
			m.reply(id, batch, "insufficient", m.cur.reason) // BAT asks again: the same answer, quietly
			return
		}
		m.pending = append(m.pending, s)
	case CommunicationSuspended:
		m.h.Warn("FC2", "circuit breaker open: batching continues with staged ingredients", batch)
		m.reply(id, batch, "staged", stagedReason)
	}
}

// verify is Idle to Verifying_Inventory: requested versus available stock and the organic
// rule are checked, and the ingredients are locked, in one transaction (Reserve).
func (m *machine) verify(s sig.Signal) {
	m.cur = &active{id: s.Str("orderId"), batch: s.Str("batchId"), items: DecodeItems(s.Str("items"))}
	m.enter(VerifyingInventory)
	if m.h.Fault("FC2") { // the warehouse does not answer
		m.h.Wait(warehouseTimeout)
		m.suspend("warehouse request timed out: circuit breaker opened")
		return
	}
	if m.h.Fault("FC1") { // an injected shortage: the real reservation then fails
		m.drain("eggs")
	}
	m.check()
}

// check asks the warehouse to reserve the ingredients of the current order and takes the
// StockCheck choice: stock >= requested, stock < requested, or the warehouse did not answer.
func (m *machine) check() {
	c := m.cur
	var ok bool
	var reason string
	err := m.call(func(ctx context.Context) error {
		var e error
		ok, reason, e = m.sql.Reserve(ctx, c.id, c.items)
		return e
	})
	switch {
	case err != nil:
		m.suspend("warehouse error: " + err.Error()) // wms_timeout
	case ok:
		m.reserved()
	default:
		m.hold(reason)
	}
}

// reserved is Reserving_Ingredients: the quantities are locked. The machine then waits for the
// physical withdrawal; BAT is told only when the stock has been updated.
func (m *machine) reserved() {
	c := m.cur
	m.enter(ReservingIngredients)
	m.h.Gauge("orders.held", 0)
	m.h.Device("WarehouseInventory", model.StateOK, "")
	_ = m.sql.SetOrderStatus(m.ctx, c.id, "reserved", "")
	if c.held {
		m.h.Log("FC1", "stock restored: held order "+c.id+" released", c.batch)
	}
	m.enter(WaitingForWithdrawal)
	m.h.Later(withdrawalTime, event{kind: evWithdrawn, order: c.id})
}

// withdrawn is the "Withdrawal confirmation": Waiting_For_Withdrawal to Updating_Inventory to Idle.
func (m *machine) withdrawn(id string) {
	if m.state != WaitingForWithdrawal || m.cur == nil || m.cur.id != id {
		return
	}
	c := m.cur
	m.enter(UpdatingInventory)
	err := m.call(func(ctx context.Context) error { return m.sql.Consume(ctx, c.id) })
	if err != nil {
		m.h.Warn("PLM", "withdrawal not booked yet, trying again: "+err.Error(), c.batch)
		m.enter(WaitingForWithdrawal)
		m.h.Later(withdrawalRetry, event{kind: evWithdrawn, order: c.id})
		return
	}
	_ = m.sql.SetOrderStatus(m.ctx, c.id, "withdrawn", "")
	m.reply(c.id, c.batch, "confirmed", "")
	m.idle()
}

// hold is Holding_Order: there is not enough stock. The order is suspended, BAT is told, and
// Logistics restocks later (the "Restock signal").
func (m *machine) hold(reason string) {
	c := m.cur
	first := !c.held
	c.held, c.reason = true, reason
	m.enter(HoldingOrder)
	m.h.Gauge("orders.held", 1)
	_ = m.sql.SetOrderStatus(m.ctx, c.id, "held", reason)
	m.h.Device("WarehouseInventory", model.StateWarn, reason)
	m.reply(c.id, c.batch, "insufficient", reason)
	if first {
		m.h.Warn("FC1", reason+": order "+c.id+" held, Logistics notified", c.batch)
	}
	m.h.Later(restockDelay, event{kind: evRestock, order: c.id})
}

// restocked is the "Restock signal": Holding_Order back to Verifying_Inventory.
func (m *machine) restocked(id string) {
	if m.state != HoldingOrder || m.cur == nil || m.cur.id != id {
		return
	}
	c := m.cur
	for _, it := range c.items {
		_ = m.sql.Restock(m.ctx, it.Ingredient, 2*it.Grams+20000)
	}
	m.h.Log("FC1", "Logistics restocked ingredients ("+c.reason+")", c.batch)
	m.enter(VerifyingInventory)
	m.check()
}

// suspend is wms_timeout: Verifying_Inventory to Communication_Suspended. BAT gets the answer
// "use staged ingredients", the circuit breaker is open, and the warehouse is tested again later.
func (m *machine) suspend(why string) {
	c := m.cur
	m.cur = nil
	m.enter(CommunicationSuspended)
	m.h.Gauge("breaker", 1)
	m.h.Device("WarehouseInventory", model.StateWarn, "warehouse timed out, breaker open")
	batch := ""
	if c != nil {
		batch = c.batch
	}
	m.h.Warn("FC2", why, batch)
	if c != nil {
		m.reply(c.id, c.batch, "staged", stagedReason)
	}
	m.h.Later(breakerWindow, event{kind: evProbe})
}

// probe tests the warehouse with one request. When it answers, wms_responsive_confirmation
// takes the machine back to Idle; otherwise it stays suspended and tests again later.
func (m *machine) probe() {
	if m.state != CommunicationSuspended {
		return
	}
	if err := m.call(func(ctx context.Context) error { _, e := m.sql.Stock(ctx); return e }); err != nil {
		m.h.Later(breakerWindow, event{kind: evProbe})
		return
	}
	m.h.Gauge("breaker", 0)
	m.h.Device("WarehouseInventory", model.StateOK, "")
	m.h.Log("FC2", "test request succeeded: circuit breaker closed", "")
	m.idle()
}

// resend sends an order again when batter preparation has not asked for its ingredients within
// 40 s. Batter preparation may have restarted and lost the order it had only received; it
// ignores an order it already knows.
func (m *machine) resend() {
	now := m.h.Now()
	for id, po := range m.sent {
		if now.Sub(po.at) > m.h.Scale(timing.OrderResend) {
			po.at = now
			m.sent[id] = po
			m.h.Warn("UC1", "order "+id+" was not picked up by batter preparation: sent again", po.sig.Str("batchId"))
			m.h.Send(po.sig)
		}
	}
}

func (m *machine) reply(orderID, batch, result, reason string) {
	m.h.Send(sig.New(sig.IngredientReq, "request", false, "orderId", orderID, "batchId", batch,
		"result", result, "reason", reason))
}

// call runs a database call with the usual time limit (timing.DBTimeout).
func (m *machine) call(f func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(m.ctx, timing.DBTimeout)
	defer cancel()
	return f(ctx)
}

// drain leaves very little of an ingredient in stock (the injected failure case FC1).
func (m *machine) drain(ing string) {
	rows, err := m.sql.Stock(m.ctx)
	if err != nil {
		return
	}
	for _, r := range rows {
		if r.Ingredient == ing && r.Grams-r.Reserved > 50 {
			_ = m.sql.Restock(m.ctx, ing, -(r.Grams - r.Reserved - 50))
		}
	}
}
