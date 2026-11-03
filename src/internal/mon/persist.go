package mon

import (
	"context"
	"log"
	"strings"
	"sync/atomic"
	"time"

	"cakefactory/internal/model"
	"cakefactory/internal/sig"
	"cakefactory/internal/sparkplug"
	"cakefactory/internal/timing"
)

// The monitor writes its events and tickets to SQL in the background. The health
// path (heartbeats, death certificates, the watchdog) must never wait for the
// database: a database outage would otherwise make the monitor believe that the
// whole line had stopped and halt it. Events and tickets get their ids in memory,
// are shown at once, and reach SQL as soon as it answers (in order, retried).

var lastDBWarn atomic.Int64

func warnDB(format string, a ...any) {
	now := time.Now().Unix()
	if last := lastDBWarn.Load(); now-last >= 5 && lastDBWarn.CompareAndSwap(last, now) {
		log.Printf(format, a...)
	}
}

func (m *Monitor) dbCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(m.Ctx, timing.DBTimeout)
}

func (m *Monitor) startDB() {
	go func() {
		for {
			select {
			case <-m.Ctx.Done():
				return
			case job := <-m.dbq:
				for !job() {
					select {
					case <-m.Ctx.Done():
						return
					case <-time.After(time.Second):
					}
				}
			}
		}
	}()
}

func (m *Monitor) dbDo(job func() bool) {
	select {
	case m.dbq <- job:
	default:
		warnDB("monitor: database queue is full, dropping a write")
	}
}

func (m *Monitor) persistEvent(ev model.LogEvent) {
	m.dbDo(func() bool {
		c, cancel := m.dbCtx()
		defer cancel()
		e := ev
		if err := m.sql.InsertEvent(c, &e); err != nil {
			warnDB("monitor: database not available for the event log (will retry): %v", err)
			return false
		}
		return true
	})
}

// closeTicketRecord marks a ticket repaired in memory and, in order, in SQL.
func (m *Monitor) closeTicketRecord(id int64) {
	now := time.Now().UTC()
	m.mu.Lock()
	for i := range m.recentTickets {
		if m.recentTickets[i].ID == id {
			m.recentTickets[i].Status, m.recentTickets[i].Closed = "repaired", &now
		}
	}
	m.mu.Unlock()
	m.dbDo(func() bool {
		m.mu.Lock()
		sqlID, ok := m.sqlTicket[id]
		m.mu.Unlock()
		if !ok {
			return true
		}
		c, cancel := m.dbCtx()
		defer cancel()
		if err := m.sql.CloseTicket(c, sqlID); err != nil {
			warnDB("monitor: database not available for tickets (will retry): %v", err)
			return false
		}
		return true
	})
}

// isHealth tells whether a message proves that the controller of a node is alive:
// its birth certificate or a heartbeat. Handoff signals do not count: a controller
// that stopped answering (FC28) may still be sending those.
func isHealth(t sparkplug.ParsedTopic, p sparkplug.Payload) bool {
	if t.Type == sparkplug.NBirth {
		return true
	}
	if t.Type != sparkplug.NData {
		return false
	}
	for _, mt := range p.Metrics {
		if strings.HasPrefix(mt.Name, sig.Heartbeat+"/") {
			return true
		}
	}
	return false
}

// touch records that a node was heard from, when the message arrives, so a busy
// worker can never make a live node look silent.
func (m *Monitor) touch(edge string) {
	m.mu.Lock()
	if n := m.nodes[edge]; n != nil && n.Status == "online" {
		n.LastSeen = time.Now()
	}
	m.mu.Unlock()
}
