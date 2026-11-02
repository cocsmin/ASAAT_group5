// Package store holds the two persistence layers of the factory:
//
//   - SQL (Microsoft SQL Server): recipes, ingredient stock, orders, the event
//     log used for traceability, maintenance tickets and shipments.
//   - Pans (MongoDB): the pan documents that travel from station to station,
//     plus a state snapshot per node so a restarted container can reload it.
//
// Both have in-process fallbacks for running without Docker.
package store

import (
	"context"
	"log"
	"os"
	"sync/atomic"
	"time"

	"cakefactory/internal/model"
)

// SQL is the relational store. Each service only calls its own methods.
type SQL interface {
	Init(ctx context.Context) error

	// Production & Logistics Management.
	Recipe(ctx context.Context, name string) (model.Recipe, error)
	Reserve(ctx context.Context, orderID string, items []model.Item) (ok bool, reason string, err error)
	Consume(ctx context.Context, orderID string) error
	Restock(ctx context.Context, ingredient string, grams float64) error
	Stock(ctx context.Context) ([]model.StockRow, error)
	SaveOrder(ctx context.Context, o model.Order) error
	LastOrderNo(ctx context.Context) (int, error) // highest PO-nnnn number stored, 0 if none
	SetOrderStatus(ctx context.Context, id, status, note string) error
	SaveShipment(ctx context.Context, s model.Shipment) error

	// Line Monitoring & Health.
	InsertEvent(ctx context.Context, e *model.LogEvent) error
	Events(ctx context.Context, limit int) ([]model.LogEvent, error)
	OpenTicket(ctx context.Context, t *model.Ticket) error
	CloseTicket(ctx context.Context, id int64) error
	Tickets(ctx context.Context, limit int) ([]model.Ticket, error)
}

// Pans is the document store for traveling pans and node state snapshots.
type Pans interface {
	Save(ctx context.Context, p model.Pan) error
	Get(ctx context.Context, id string) (model.Pan, bool, error)
	List(ctx context.Context, limit int) ([]model.Pan, error)
	SaveState(ctx context.Context, node string, state map[string]int64) error
	LoadState(ctx context.Context, node string) (map[string]int64, error)
}

// Move records that a pan arrived at a station with a new status and returns
// the updated pan. mutate may adjust other fields before it is saved.
func Move(ctx context.Context, s Pans, id, station, status, note string, mutate func(*model.Pan)) model.Pan {
	p, ok, err := s.Get(ctx, id)
	if err != nil {
		warnPans("pans: document store not available (get %s): %v", id, err)
	}
	if !ok {
		p = model.Pan{ID: id}
	}
	p.Station, p.Status, p.Updated = station, status, time.Now().UTC()
	p.History = append(p.History, model.Step{At: p.Updated, Station: station, Status: status, Note: note})
	if len(p.History) > 40 {
		p.History = p.History[len(p.History)-40:]
	}
	if mutate != nil {
		mutate(&p)
	}
	if err := s.Save(ctx, p); err != nil {
		warnPans("pans: document store not available (save %s): %v", id, err)
	}
	return p
}

// OpenSQL connects to SQL Server when SQL_DSN is set, otherwise it returns an
// in-memory store. It retries while the database container is starting.
func OpenSQL(ctx context.Context) (SQL, error) {
	dsn := os.Getenv("SQL_DSN")
	var s SQL
	if dsn == "" {
		log.Println("SQL_DSN not set: using in-memory SQL store")
		s = NewMemSQL()
	} else {
		s = NewMSSQL(dsn)
	}
	for {
		err := s.Init(ctx)
		if err == nil {
			return s, nil
		}
		log.Printf("sql: not ready yet: %v", err)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
}

// OpenPans connects to MongoDB when MONGO_URI is set, otherwise it stores pan
// documents as JSON files under PAN_DIR (default ./data).
func OpenPans(ctx context.Context) (Pans, error) {
	if uri := os.Getenv("MONGO_URI"); uri != "" {
		for {
			p, err := NewMongo(ctx, uri)
			if err == nil {
				return p, nil
			}
			log.Printf("mongo: not ready yet: %v", err)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(3 * time.Second):
			}
		}
	}
	dir := os.Getenv("PAN_DIR")
	if dir == "" {
		dir = "data"
	}
	log.Printf("MONGO_URI not set: storing pan documents as files in %s", dir)
	return NewFilePans(dir)
}

var lastPansWarn atomic.Int64

// warnPans logs a document store problem at most once every 5 s.
func warnPans(format string, a ...any) {
	now := time.Now().Unix()
	if last := lastPansWarn.Load(); now-last >= 5 && lastPansWarn.CompareAndSwap(last, now) {
		log.Printf(format, a...)
	}
}
