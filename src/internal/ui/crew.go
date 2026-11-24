package ui

import (
	"context"
	"log"
	"math/rand"
	"strings"
	"time"

	"cakefactory/internal/sim"
)

// crew plays the maintenance crew when the factory fails by itself (FAULT_RATE): every open
// ticket is repaired after a random time, on average CREW_SECONDS of simulated time (a real
// crew needs minutes to hours; the simulation is compressed like the rest). Without CREW_SECONDS
// a person presses "Mark repaired" on the dashboard.
func (s *Server) crew(mean time.Duration) {
	seen := map[int64]time.Time{} // ticket -> when its repair is due
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	log.Printf("ui: maintenance crew on, a repair takes %s on average (simulated time)", mean)
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-t.C:
		}
		src := s.source()
		if src == nil {
			continue
		}
		ctx, cancel := context.WithTimeout(s.ctx, 3*time.Second)
		tickets, err := src.Tickets(ctx, 100)
		cancel()
		if err != nil {
			continue
		}
		for _, tk := range tickets {
			if tk.Status != "open" || strings.HasSuffix(tk.MachineID, "-container") { // Docker restarts containers
				continue
			}
			due, ok := seen[tk.ID]
			if !ok {
				due = time.Now().Add(sim.Scale(time.Duration((0.5 + rand.Float64()) * float64(mean))))
				seen[tk.ID] = due
			}
			if time.Now().After(due) {
				if err := s.repair(tk.ID); err != nil {
					log.Printf("ui: crew could not repair %s: %v", tk.MachineID, err)
					continue
				}
				delete(seen, tk.ID)
				log.Printf("ui: crew repaired %s (ticket %d)", tk.MachineID, tk.ID)
			}
		}
	}
}
