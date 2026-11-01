package store

import (
	"context"
	"fmt"
	"sync"
	"time"

	"cakefactory/internal/model"
)

// SeedRecipe is the one recipe the simulated factory bakes.
var SeedRecipe = model.Recipe{
	Name: "Vanilla sponge",
	Items: []model.Item{
		{Ingredient: "flour", Grams: 1000},
		{Ingredient: "sugar", Grams: 500},
		{Ingredient: "eggs", Grams: 400},
		{Ingredient: "butter", Grams: 300},
		{Ingredient: "milk", Grams: 250},
	},
	MixSeconds: 6, DoseTolPct: 2, PansPerBatch: 6, FillTarget: 450, FillTol: 5,
}

// SeedStockGrams is the starting stock of every ingredient.
const SeedStockGrams = 9000

// MemSQL is an in-memory SQL stand-in for running without Docker.
type MemSQL struct {
	mu      sync.Mutex
	stock   map[string]*model.StockRow
	holds   map[string][]model.Item
	done    map[string]bool
	orders  map[string]model.Order
	ships   []model.Shipment
	events  []model.LogEvent
	tickets []model.Ticket
}

// NewMemSQL creates the in-memory store.
func NewMemSQL() *MemSQL { return &MemSQL{} }

// Init seeds the stock.
func (m *MemSQL) Init(context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stock = map[string]*model.StockRow{}
	m.holds = map[string][]model.Item{}
	m.done = map[string]bool{}
	m.orders = map[string]model.Order{}
	for _, it := range SeedRecipe.Items {
		m.stock[it.Ingredient] = &model.StockRow{Ingredient: it.Ingredient, Grams: SeedStockGrams, Organic: true}
	}
	return nil
}

// Recipe returns the seed recipe.
func (m *MemSQL) Recipe(_ context.Context, name string) (model.Recipe, error) {
	if name != SeedRecipe.Name {
		return model.Recipe{}, fmt.Errorf("unknown recipe %q", name)
	}
	return SeedRecipe, nil
}

// Reserve reserves ingredients atomically.
func (m *MemSQL) Reserve(_ context.Context, orderID string, items []model.Item) (bool, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.done[orderID] || m.holds[orderID] != nil { // repeated request: same answer
		return true, "", nil
	}
	for _, it := range items {
		r, ok := m.stock[it.Ingredient]
		switch {
		case !ok:
			return false, "unknown ingredient " + it.Ingredient, nil
		case !r.Organic:
			return false, it.Ingredient + " is not organic", nil
		case r.Grams-r.Reserved < it.Grams:
			return false, "insufficient stock of " + it.Ingredient, nil
		}
	}
	for _, it := range items {
		m.stock[it.Ingredient].Reserved += it.Grams
	}
	m.holds[orderID] = items
	return true, "", nil
}

// Consume turns a reservation into a withdrawal.
func (m *MemSQL) Consume(_ context.Context, orderID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, it := range m.holds[orderID] {
		r := m.stock[it.Ingredient]
		r.Reserved -= it.Grams
		r.Grams -= it.Grams
	}
	delete(m.holds, orderID)
	m.done[orderID] = true
	return nil
}

// Restock adds stock.
func (m *MemSQL) Restock(_ context.Context, ing string, grams float64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.stock[ing]; ok {
		r.Grams += grams
	}
	return nil
}

// Stock lists the stock.
func (m *MemSQL) Stock(context.Context) ([]model.StockRow, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []model.StockRow
	for _, it := range SeedRecipe.Items {
		out = append(out, *m.stock[it.Ingredient])
	}
	return out, nil
}

// SaveOrder stores an order.
func (m *MemSQL) SaveOrder(_ context.Context, o model.Order) error {
	m.mu.Lock()
	m.orders[o.ID] = o
	m.mu.Unlock()
	return nil
}

// SetOrderStatus updates an order.
func (m *MemSQL) SetOrderStatus(_ context.Context, id, status, note string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	o := m.orders[id]
	o.Status, o.Note = status, note
	m.orders[id] = o
	return nil
}

// SaveShipment stores a shipment.
func (m *MemSQL) SaveShipment(_ context.Context, s model.Shipment) error {
	m.mu.Lock()
	m.ships = append(m.ships, s)
	m.mu.Unlock()
	return nil
}

// InsertEvent stores an event and assigns its id.
func (m *MemSQL) InsertEvent(_ context.Context, e *model.LogEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	e.ID = int64(len(m.events) + 1)
	m.events = append(m.events, *e)
	return nil
}

// Events returns the newest events first.
func (m *MemSQL) Events(_ context.Context, limit int) ([]model.LogEvent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []model.LogEvent
	for i := len(m.events) - 1; i >= 0 && len(out) < limit; i-- {
		out = append(out, m.events[i])
	}
	return out, nil
}

// OpenTicket stores a ticket and assigns its id.
func (m *MemSQL) OpenTicket(_ context.Context, t *model.Ticket) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	t.ID = int64(len(m.tickets) + 1)
	t.Status = "open"
	t.Opened = time.Now().UTC()
	m.tickets = append(m.tickets, *t)
	return nil
}

// CloseTicket marks a ticket repaired.
func (m *MemSQL) CloseTicket(_ context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.tickets {
		if m.tickets[i].ID == id {
			now := time.Now().UTC()
			m.tickets[i].Status, m.tickets[i].Closed = "repaired", &now
		}
	}
	return nil
}

// Tickets returns the newest tickets first.
func (m *MemSQL) Tickets(_ context.Context, limit int) ([]model.Ticket, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []model.Ticket
	for i := len(m.tickets) - 1; i >= 0 && len(out) < limit; i-- {
		out = append(out, m.tickets[i])
	}
	return out, nil
}
