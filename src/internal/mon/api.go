package mon

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
)

// serve answers the monitor's small API: the Maintenance action "repair confirmed".
func (m *Monitor) serve(addr string) {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, "ok") })
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

func reply(w http.ResponseWriter, v any, err error) {
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
