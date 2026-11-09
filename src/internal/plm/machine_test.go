package plm

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"cakefactory/internal/model"
	"cakefactory/internal/sig"
	"cakefactory/internal/store"
	"cakefactory/internal/timing"
)

// fakeHost records what the state machine does. Events that the machine asks for with Later are
// kept in later; the tests hand them to the machine themselves, in the order they choose.
type fakeHost struct {
	sent   []sig.Signal
	logs   []string
	warns  []string
	gauges map[string]int64
	faults map[string]int
	online bool
	later  []event
	waits  []time.Duration
	now    time.Time
}

func (h *fakeHost) Send(s sig.Signal)                   { h.sent = append(h.sent, s) }
func (h *fakeHost) Log(code, text, batch string)        { h.logs = append(h.logs, code+" "+text) }
func (h *fakeHost) Warn(code, text, batch string)       { h.warns = append(h.warns, code+" "+text) }
func (h *fakeHost) Device(name, state, detail string)   {}
func (h *fakeHost) Gauge(name string, v int64)          { h.gauges[name] = v }
func (h *fakeHost) Online() bool                        { return h.online }
func (h *fakeHost) Wait(d time.Duration) bool           { h.waits = append(h.waits, d); return true }
func (h *fakeHost) Later(_ time.Duration, ev event)     { h.later = append(h.later, ev) }
func (h *fakeHost) Now() time.Time                      { return h.now }
func (h *fakeHost) Scale(d time.Duration) time.Duration { return d }
func (h *fakeHost) Fault(code string) bool {
	if h.faults[code] > 0 {
		h.faults[code]--
		return true
	}
	return false
}

// orders returns the production orders sent to BAT.
func (h *fakeHost) orders() []sig.Signal {
	var out []sig.Signal
	for _, s := range h.sent {
		if s.Name == sig.OrderAndRecipe {
			out = append(out, s)
		}
	}
	return out
}

// answers returns the results sent to BAT for one order.
func (h *fakeHost) answers(order string) []string {
	var out []string
	for _, s := range h.sent {
		if s.Name == sig.IngredientReq && s.Str("orderId") == order {
			out = append(out, s.Str("result"))
		}
	}
	return out
}

func (h *fakeHost) countWarns(prefix string) int {
	n := 0
	for _, w := range h.warns {
		if strings.HasPrefix(w, prefix) {
			n++
		}
	}
	return n
}

func (h *fakeHost) hasLater(k evKind) bool {
	for _, ev := range h.later {
		if ev.kind == k {
			return true
		}
	}
	return false
}

func setup(t *testing.T) (*machine, *fakeHost, *store.MemSQL) {
	t.Helper()
	sql := store.NewMemSQL()
	if err := sql.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	h := &fakeHost{gauges: map[string]int64{}, faults: map[string]int{}, online: true, now: time.Unix(1000, 0)}
	return newMachine(h, sql, context.Background()), h, sql
}

func request(order, batch string) event {
	return event{kind: evRequest, req: sig.New(sig.IngredientReq, "request", true, "orderId", order, "batchId", batch,
		"items", encodeItems(store.SeedRecipe.Items))}
}

func TestStateNamesAreTheOnesOfTheDiagram(t *testing.T) {
	want := []State{"Idle", "Scheduling_Order", "Fetching_recipe_targets", "Verifying_Inventory", "Reserving_Ingredients",
		"Waiting_For_Withdrawal", "Updating_Inventory", "Holding_Order", "Communication_Suspended"}
	seen := map[int64]bool{}
	for _, s := range want {
		n, ok := stateNo[s]
		if !ok || seen[n] {
			t.Fatalf("state %s has no own gauge value", s)
		}
		seen[n] = true
	}
	if len(stateNo) != len(want) {
		t.Fatalf("%d states in the table, the diagram has %d", len(stateNo), len(want))
	}
}

func TestFirstOrderIsCreatedPendingAndReleasedToBAT(t *testing.T) {
	m, h, sql := setup(t)
	m.handle(event{kind: evOrderTimer})
	if m.state != Idle {
		t.Fatalf("after sending the order the machine is %s, not Idle", m.state)
	}
	o := h.orders()
	if len(o) != 1 || o[0].Str("orderId") != "PO-0001" || o[0].Str("batchId") != "B0001" {
		t.Fatalf("orders sent: %+v", o)
	}
	if got, ok := sql.Order("PO-0001"); !ok || got.Status != "released" {
		t.Fatalf("stored order: %+v", got)
	}
	if h.gauges["orders.created"] != 1 || h.gauges["plm.state"] != stateNo[Idle] {
		t.Fatalf("gauges: %+v", h.gauges)
	}
}

func TestNoOrderWhileBATHasTwoOrCheckOnlyWhenTheMonitorIsOnline(t *testing.T) {
	m, h, _ := setup(t)
	h.online = false
	m.handle(event{kind: evOrderTimer})
	if len(h.orders()) != 0 {
		t.Fatal("no production without the monitor")
	}
	h.online = true
	m.handle(event{kind: evOrderTimer})
	m.handle(event{kind: evOrderTimer})
	m.handle(event{kind: evOrderTimer})
	if got := len(h.orders()); got != maxOutstanding {
		t.Fatalf("%d orders sent while BAT has not asked for any, the limit is %d", got, maxOutstanding)
	}
}

func TestConfirmationComesOnlyAfterTheWithdrawalAndTheStockUpdate(t *testing.T) {
	m, h, sql := setup(t)
	m.handle(event{kind: evOrderTimer})
	m.handle(request("PO-0001", "B0001"))
	if m.state != WaitingForWithdrawal {
		t.Fatalf("state %s, want Waiting_For_Withdrawal", m.state)
	}
	if len(h.answers("PO-0001")) != 0 || !h.hasLater(evWithdrawn) {
		t.Fatalf("BAT must not be answered before the withdrawal: %v", h.answers("PO-0001"))
	}
	rows, _ := sql.Stock(context.Background())
	if rows[0].Reserved == 0 {
		t.Fatal("the ingredients are locked while the machine waits")
	}
	m.handle(event{kind: evWithdrawn, order: "PO-0001"})
	if m.state != Idle || strings.Join(h.answers("PO-0001"), ",") != "confirmed" {
		t.Fatalf("state %s, answers %v", m.state, h.answers("PO-0001"))
	}
	rows, _ = sql.Stock(context.Background())
	if rows[0].Reserved != 0 || rows[0].Grams != store.SeedStockGrams-4000 {
		t.Fatalf("stock after the withdrawal: %+v", rows[0])
	}
	if got, _ := sql.Order("PO-0001"); got.Status != "withdrawn" {
		t.Fatalf("order status %q", got.Status)
	}
}

func TestOrderCycleThatRunsOutWhileBusyIsTakenUpWhenIdle(t *testing.T) {
	m, h, _ := setup(t)
	m.handle(event{kind: evOrderTimer})
	m.handle(request("PO-0001", "B0001"))
	m.handle(event{kind: evOrderTimer}) // the cycle ends while the machine waits for the withdrawal
	if len(h.orders()) != 1 {
		t.Fatal("no new order while waiting for a withdrawal")
	}
	m.handle(event{kind: evWithdrawn, order: "PO-0001"})
	if len(h.orders()) != 2 {
		t.Fatalf("the order of the missed cycle is created when the machine is Idle again: %d", len(h.orders()))
	}
}

func TestNotEnoughStockHoldsTheOrderUntilTheRestock(t *testing.T) {
	m, h, sql := setup(t)
	_ = sql.Restock(context.Background(), "eggs", -35000)
	m.handle(event{kind: evOrderTimer})
	m.handle(request("PO-0001", "B0001"))
	if m.state != HoldingOrder || h.gauges["orders.held"] != 1 || !h.hasLater(evRestock) {
		t.Fatalf("state %s gauges %v", m.state, h.gauges)
	}
	if strings.Join(h.answers("PO-0001"), ",") != "insufficient" || h.countWarns("FC1") != 1 {
		t.Fatalf("answers %v warnings %v", h.answers("PO-0001"), h.warns)
	}
	if got, _ := sql.Order("PO-0001"); got.Status != "held" {
		t.Fatalf("order status %q", got.Status)
	}
	m.handle(request("PO-0001", "B0001")) // BAT asks again: the same answer, no second warning
	if strings.Join(h.answers("PO-0001"), ",") != "insufficient,insufficient" || h.countWarns("FC1") != 1 {
		t.Fatalf("answers %v warnings %v", h.answers("PO-0001"), h.warns)
	}
	m.handle(event{kind: evOrderTimer}) // no new order while one is held
	if len(h.orders()) != 1 {
		t.Fatal("no new order while an order is held")
	}
	m.handle(event{kind: evRestock, order: "PO-0001"})
	if m.state != WaitingForWithdrawal || h.gauges["orders.held"] != 0 {
		t.Fatalf("after the restock: state %s gauges %v", m.state, h.gauges)
	}
	m.handle(event{kind: evWithdrawn, order: "PO-0001"})
	if strings.Join(h.answers("PO-0001"), ",") != "insufficient,insufficient,confirmed" || m.state != Idle {
		t.Fatalf("answers %v state %s", h.answers("PO-0001"), m.state)
	}
}

func TestStillNotEnoughAfterTheRestockHoldsTheOrderAgain(t *testing.T) {
	m, h, sql := setup(t)
	_ = sql.Restock(context.Background(), "eggs", -35000)
	m.handle(event{kind: evOrderTimer})
	m.handle(request("PO-0001", "B0001"))
	_ = sql.Restock(context.Background(), "eggs", -1000000) // the restock cannot make up for this
	h.later = nil
	m.handle(event{kind: evRestock, order: "PO-0001"})
	if m.state == HoldingOrder && !h.hasLater(evRestock) {
		t.Fatal("a held order that is still short asks for the next restock")
	}
}

func TestWarehouseTimeoutSuspendsCommunicationAndTheProbeResumesIt(t *testing.T) {
	m, h, _ := setup(t)
	h.faults["FC2"] = 1
	m.handle(event{kind: evOrderTimer})
	m.handle(request("PO-0001", "B0001"))
	if m.state != CommunicationSuspended || h.gauges["breaker"] != 1 || !h.hasLater(evProbe) {
		t.Fatalf("state %s gauges %v", m.state, h.gauges)
	}
	if len(h.waits) != 1 || h.waits[0] != warehouseTimeout {
		t.Fatalf("the request waited %v, the timeout is %v", h.waits, warehouseTimeout)
	}
	if strings.Join(h.answers("PO-0001"), ",") != "staged" {
		t.Fatalf("answers %v", h.answers("PO-0001"))
	}
	m.handle(event{kind: evOrderTimer}) // the machine only creates orders when it is Idle
	m.handle(request("PO-0002", "B0002"))
	if strings.Join(h.answers("PO-0002"), ",") != "staged" || len(h.orders()) != 1 {
		t.Fatalf("while suspended: answers %v, orders %d", h.answers("PO-0002"), len(h.orders()))
	}
	m.handle(event{kind: evProbe})
	if m.state != Idle || h.gauges["breaker"] != 0 {
		t.Fatalf("after the probe: state %s gauges %v", m.state, h.gauges)
	}
	if len(h.orders()) != 2 {
		t.Fatalf("the order of the cycle that ran out while suspended is created when Idle again: %d", len(h.orders()))
	}
}

// brokenSQL fails the calls the tests name.
type brokenSQL struct {
	store.SQL
	reserve, recipe, stock bool
}

func (b brokenSQL) Reserve(ctx context.Context, id string, it []model.Item) (bool, string, error) {
	if b.reserve {
		return false, "", errors.New("database down")
	}
	return b.SQL.Reserve(ctx, id, it)
}

func (b brokenSQL) Recipe(ctx context.Context, name string) (model.Recipe, error) {
	if b.recipe {
		return model.Recipe{}, errors.New("database down")
	}
	return b.SQL.Recipe(ctx, name)
}

func (b brokenSQL) Stock(ctx context.Context) ([]model.StockRow, error) {
	if b.stock {
		return nil, errors.New("database down")
	}
	return b.SQL.Stock(ctx)
}

func TestDatabaseErrorIsTreatedAsAWarehouseTimeout(t *testing.T) {
	m, h, sql := setup(t)
	b := brokenSQL{SQL: sql, reserve: true, stock: true}
	m.sql = b
	m.handle(event{kind: evOrderTimer})
	m.handle(request("PO-0001", "B0001"))
	if m.state != CommunicationSuspended || strings.Join(h.answers("PO-0001"), ",") != "staged" {
		t.Fatalf("state %s answers %v", m.state, h.answers("PO-0001"))
	}
	h.later = nil
	m.handle(event{kind: evProbe}) // the warehouse still does not answer
	if m.state != CommunicationSuspended || !h.hasLater(evProbe) {
		t.Fatal("the machine tests the warehouse again later")
	}
	b.stock = false
	m.sql = b
	m.handle(event{kind: evProbe})
	if m.state != Idle {
		t.Fatalf("state %s", m.state)
	}
}

func TestOrderIsCancelledWhenTheRecipeCannotBeRead(t *testing.T) {
	m, h, sql := setup(t)
	m.sql = brokenSQL{SQL: sql, recipe: true}
	m.handle(event{kind: evOrderTimer})
	if len(h.orders()) != 0 || m.state != Idle {
		t.Fatalf("orders %d, state %s", len(h.orders()), m.state)
	}
	if got, _ := sql.Order("PO-0001"); got.Status != "cancelled" {
		t.Fatalf("order status %q", got.Status)
	}
	if h.countWarns("PLM") != 1 {
		t.Fatalf("warnings %v", h.warns)
	}
}

func TestRequestForAnotherOrderWaitsUntilTheMachineIsIdle(t *testing.T) {
	m, h, _ := setup(t)
	m.handle(event{kind: evOrderTimer})
	m.handle(event{kind: evOrderTimer})
	m.handle(request("PO-0001", "B0001"))
	m.handle(request("PO-0002", "B0002"))
	if len(h.answers("PO-0002")) != 0 {
		t.Fatal("the second request waits")
	}
	m.handle(request("PO-0001", "B0001")) // a repeated request of the same order changes nothing
	m.handle(event{kind: evWithdrawn, order: "PO-0001"})
	if m.state != WaitingForWithdrawal || m.cur.id != "PO-0002" {
		t.Fatalf("state %s, current %+v", m.state, m.cur)
	}
	m.handle(event{kind: evWithdrawn, order: "PO-0002"})
	if strings.Join(h.answers("PO-0001"), ",") != "confirmed" || strings.Join(h.answers("PO-0002"), ",") != "confirmed" {
		t.Fatalf("answers %v %v", h.answers("PO-0001"), h.answers("PO-0002"))
	}
}

func TestEventsThatDoNotBelongToTheStateAreIgnored(t *testing.T) {
	m, h, _ := setup(t)
	m.handle(event{kind: evWithdrawn, order: "PO-0009"})
	m.handle(event{kind: evRestock, order: "PO-0009"})
	m.handle(event{kind: evProbe})
	if m.state != Idle || len(h.sent) != 0 {
		t.Fatalf("state %s, signals sent %d", m.state, len(h.sent))
	}
	m.handle(event{kind: evOrderTimer})
	m.handle(request("PO-0001", "B0001"))
	m.handle(event{kind: evWithdrawn, order: "PO-0009"}) // not the order the machine waits for
	if m.state != WaitingForWithdrawal {
		t.Fatalf("state %s", m.state)
	}
}

func TestOrderThatBATDoesNotPickUpIsSentAgainAfter40Seconds(t *testing.T) {
	m, h, _ := setup(t)
	m.handle(event{kind: evOrderTimer})
	h.now = h.now.Add(30 * time.Second)
	m.handle(event{kind: evResend})
	if len(h.orders()) != 1 {
		t.Fatal("too early to send it again")
	}
	h.now = h.now.Add(11 * time.Second)
	m.handle(event{kind: evResend})
	if len(h.orders()) != 2 || h.countWarns("UC1") != 1 {
		t.Fatalf("orders sent %d, warnings %v", len(h.orders()), h.warns)
	}
	m.handle(event{kind: evResend})
	if len(h.orders()) != 2 {
		t.Fatal("it is sent again only every 40 s")
	}
}

func TestInjectedShortageHoldsTheOrder(t *testing.T) {
	m, h, _ := setup(t)
	h.faults["FC1"] = 1
	m.handle(event{kind: evOrderTimer})
	m.handle(request("PO-0001", "B0001"))
	if m.state != HoldingOrder || strings.Join(h.answers("PO-0001"), ",") != "insufficient" {
		t.Fatalf("state %s answers %v", m.state, h.answers("PO-0001"))
	}
}

func TestFinalAnswerComesBeforeBATRepeatsItsRequest(t *testing.T) {
	// The slowest answer is the one after a held order: the restock and then the withdrawal.
	// BAT repeats an unanswered request after timing.IngredientRetry; a repeat is harmless (it is
	// answered quietly) but the UPPAAL model (docs/uppaal/plm.xml, formula 5) assumes none is needed.
	if got := restockDelay + withdrawalTime; got >= timing.IngredientRetry {
		t.Fatalf("restock + withdrawal = %v, BAT repeats its request after %v", got, timing.IngredientRetry)
	}
	if got := warehouseTimeout; got >= timing.IngredientRetry {
		t.Fatalf("warehouse timeout %v is not below the repeat time %v", got, timing.IngredientRetry)
	}
}

func TestRestartWhileWaitingForTheWithdrawalLosesNothing(t *testing.T) {
	// The reservation is in the database; the restarted machine knows nothing. BAT repeats its
	// request, Reserve gives the same answer, the withdrawal is booked once.
	m, h, sql := setup(t)
	m.handle(event{kind: evOrderTimer})
	m.handle(request("PO-0001", "B0001"))
	if m.state != WaitingForWithdrawal {
		t.Fatalf("state %s", m.state)
	}
	h2 := &fakeHost{gauges: map[string]int64{}, faults: map[string]int{}, online: true, now: h.now}
	m2 := newMachine(h2, sql, context.Background()) // the restarted PLM-WH
	m2.handle(request("PO-0001", "B0001"))
	if m2.state != WaitingForWithdrawal {
		t.Fatalf("after the restart: %s", m2.state)
	}
	m2.handle(event{kind: evWithdrawn, order: "PO-0001"})
	if strings.Join(h2.answers("PO-0001"), ",") != "confirmed" {
		t.Fatalf("answers %v", h2.answers("PO-0001"))
	}
	rows, _ := sql.Stock(context.Background())
	if rows[0].Grams != store.SeedStockGrams-4000 || rows[0].Reserved != 0 {
		t.Fatalf("flour taken once: %+v", rows[0])
	}
}
