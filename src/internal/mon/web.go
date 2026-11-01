package mon

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"sort"
	"time"

	"cakefactory/internal/model"
)

//go:embed web
var webFS embed.FS

// snapshot is everything the dashboard shows, as one JSON document.
type snapshot struct {
	Now      time.Time               `json:"now"`
	Hb       float64                 `json:"heartbeatSeconds"`
	Nodes    []NodeState             `json:"nodes"`
	Sections []Section               `json:"sections"`
	Alerts   []Alert                 `json:"alerts"`
	Events   []model.LogEvent        `json:"events"`
	Tickets  []model.Ticket          `json:"tickets"`
	Discards map[string]int          `json:"discards"`
	Faults   []model.FaultInfo       `json:"faults"`
	Open     map[string]model.Ticket `json:"-"`
}

func (m *Monitor) snapshot() snapshot {
	tickets, _ := m.sql.Tickets(m.Ctx, 30)
	m.mu.Lock()
	defer m.mu.Unlock()
	if tickets == nil {
		tickets = []model.Ticket{}
	}
	s := snapshot{Events: []model.LogEvent{}, Now: time.Now().UTC(), Hb: m.hb.Seconds(), Discards: map[string]int{}, Faults: model.FaultCatalog, Tickets: tickets}
	for _, id := range model.Nodes {
		if n, ok := m.nodes[id]; ok {
			cp := *n
			cp.Devices = map[string]DevState{}
			for k, v := range n.Devices {
				cp.Devices[k] = v
			}
			cp.Gauges = map[string]int64{}
			for k, v := range n.Gauges {
				cp.Gauges[k] = v
			}
			s.Nodes = append(s.Nodes, cp)
		}
	}
	for i := 1; i <= 5; i++ {
		s.Sections = append(s.Sections, m.sections[i])
	}
	for k, v := range m.discards {
		s.Discards[k] = v
	}
	from := len(m.events) - 80
	if from < 0 {
		from = 0
	}
	for i := len(m.events) - 1; i >= from; i-- {
		s.Events = append(s.Events, m.events[i])
	}
	s.Alerts = m.alerts()
	return s
}

// alerts lists what the overseer must look at: open tickets, nodes that do not
// answer, band sections that are not running and recent warnings.
func (m *Monitor) alerts() []Alert {
	out := []Alert{}
	for _, t := range m.tickets {
		out = append(out, Alert{Level: "error", Title: "Maintenance needed: " + t.MachineID, Detail: t.Reason, Node: t.Node, At: t.Opened})
	}
	for _, n := range m.nodes {
		switch n.Status {
		case "silent":
			out = append(out, Alert{Level: "error", Title: n.ID + " is not responding", Detail: "no heartbeat received", Node: n.ID, At: n.LastSeen})
		case "dead":
			out = append(out, Alert{Level: "error", Title: n.ID + " container is down", Detail: "death certificate received; waiting for Docker to restart it", Node: n.ID, At: n.deadAt})
		}
	}
	for i := 1; i <= 5; i++ {
		sec := m.sections[i]
		if sec.State != "run" {
			out = append(out, Alert{Level: "warning", Title: fmt.Sprintf("Band section %d (%s): %s", i, sec.Name, sec.State), Detail: "", Node: sec.Owner, At: time.Now()})
		}
	}
	keep := m.warnings[:0]
	for _, w := range m.warnings {
		if time.Since(w.At) < 45*time.Second {
			keep = append(keep, w)
		}
	}
	m.warnings = keep
	for i := len(m.warnings) - 1; i >= 0 && len(out) < 14; i-- {
		out = append(out, m.warnings[i])
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Level == "error" && out[j].Level != "error" })
	return out
}

func (m *Monitor) broadcast(event string, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	msg := []byte("event: " + event + "\ndata: " + string(b) + "\n\n")
	m.mu.Lock()
	defer m.mu.Unlock()
	for c := range m.subs {
		select {
		case c <- msg:
		default: // slow client: drop, it gets the next snapshot
		}
	}
}

func (m *Monitor) hasSubs() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.subs) > 0
}

func (m *Monitor) push() {
	if m.hasSubs() {
		m.broadcast("snapshot", m.snapshot())
	}
}

func (m *Monitor) pushEvent(ev model.LogEvent) {
	if m.hasSubs() {
		m.broadcast("event", ev)
		m.broadcast("snapshot", m.snapshot())
	}
}

func (m *Monitor) serve(addr string) {
	sub, _ := fs.Sub(webFS, "web")
	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.FS(sub)))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, "ok") })
	mux.HandleFunc("/api/state", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, m.snapshot()) })
	mux.HandleFunc("/api/pans", func(w http.ResponseWriter, r *http.Request) {
		pans, err := m.Pans.List(r.Context(), 40)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		writeJSON(w, pans)
	})
	mux.HandleFunc("/api/stock", func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		g := map[string]int64{}
		if n := m.nodes[model.PLM]; n != nil {
			for k, v := range n.Gauges {
				g[k] = v
			}
		}
		m.mu.Unlock()
		writeJSON(w, g)
	})
	mux.HandleFunc("/api/stream", m.stream)
	mux.HandleFunc("/api/inject", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Node  string `json:"node"`
			Code  string `json:"code"`
			Count int    `json:"count"`
		}
		if r.Method != http.MethodPost || json.NewDecoder(r.Body).Decode(&in) != nil {
			http.Error(w, "POST {node, code, count}", 400)
			return
		}
		if err := m.Inject(in.Node, in.Code, in.Count); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
	})
	mux.HandleFunc("/api/repair", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Ticket int64 `json:"ticket"`
		}
		if r.Method != http.MethodPost || json.NewDecoder(r.Body).Decode(&in) != nil {
			http.Error(w, "POST {ticket}", 400)
			return
		}
		if err := m.Repair(in.Ticket); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
	})
	srv := &http.Server{Addr: addr, Handler: mux}
	go func() {
		<-m.Ctx.Done()
		srv.Close()
	}()
	log.Printf("dashboard on %s", addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (m *Monitor) stream(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", 500)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	ch := make(chan []byte, 64)
	m.mu.Lock()
	m.subs[ch] = struct{}{}
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		delete(m.subs, ch)
		m.mu.Unlock()
	}()
	b, _ := json.Marshal(m.snapshot())
	fmt.Fprintf(w, "event: snapshot\ndata: %s\n\n", b)
	fl.Flush()
	for {
		select {
		case <-r.Context().Done():
			return
		case msg := <-ch:
			w.Write(msg)
			fl.Flush()
		}
	}
}
