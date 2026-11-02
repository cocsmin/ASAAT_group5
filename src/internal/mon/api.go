package mon

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"
)

// serve answers the monitor's small API: the read side of the event log and the
// tickets (for the experiment dashboard when there is no SQL Server to read) and
// the Maintenance action "repair confirmed".
func (m *Monitor) serve(addr string) {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, "ok") })
	mux.HandleFunc("/api/events", func(w http.ResponseWriter, r *http.Request) {
		c, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		ev, err := m.sql.Events(c, limit(r, 80))
		reply(w, ev, err)
	})
	mux.HandleFunc("/api/tickets", func(w http.ResponseWriter, r *http.Request) {
		c, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		tk, err := m.sql.Tickets(c, limit(r, 30))
		reply(w, tk, err)
	})
	mux.HandleFunc("/api/discards", func(w http.ResponseWriter, _ *http.Request) {
		m.mu.Lock()
		out := map[string]int{}
		for k, v := range m.discards {
			out[k] = v
		}
		m.mu.Unlock()
		reply(w, out, nil)
	})
	mux.HandleFunc("/api/repair", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Ticket    int64  `json:"ticket"`
			MachineID string `json:"machineId"`
		}
		if r.Method != http.MethodPost || json.NewDecoder(r.Body).Decode(&in) != nil {
			http.Error(w, "POST {ticket} or {machineId}", http.StatusBadRequest)
			return
		}
		var err error
		if in.MachineID != "" {
			err = m.RepairMachine(in.MachineID)
		} else {
			err = m.Repair(in.Ticket)
		}
		reply(w, map[string]bool{"ok": true}, err)
	})
	srv := &http.Server{Addr: addr, Handler: mux}
	go func() {
		<-m.Ctx.Done()
		srv.Close()
	}()
	log.Printf("monitor API on %s", addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func limit(r *http.Request, def int) int {
	if n, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && n > 0 && n <= 500 {
		return n
	}
	return def
}

func reply(w http.ResponseWriter, v any, err error) {
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
