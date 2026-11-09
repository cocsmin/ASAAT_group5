// Package ui is the experiment dashboard. It is not part of the factory: it only
// watches it and runs the experiments. It reads the event log and the tickets from
// SQL Server (or, without a database, from the monitor's read API), the pans and
// cakes from MongoDB, and the live state of the systems by listening to the
// Sparkplug B messages. It publishes fault injections and calls MON only for the
// Maintenance action "repair confirmed". It keeps working when MON is down.
package ui

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"

	"cakefactory/internal/model"
	"cakefactory/internal/sig"
	"cakefactory/internal/sparkplug"
	"cakefactory/internal/store"
)

//go:embed web
var webFS embed.FS

// logSource is where the event log and the tickets are read from.
type logSource interface {
	Events(ctx context.Context, limit int) ([]model.LogEvent, error)
	Tickets(ctx context.Context, limit int) ([]model.Ticket, error)
	DiscardCounts(ctx context.Context) (map[string]int, error)
}

// Alert is something the production overseer should look at.
type Alert struct {
	Level  string    `json:"level"` // warning | error
	Title  string    `json:"title"`
	Detail string    `json:"detail"`
	Node   string    `json:"node"`
	At     time.Time `json:"at"`
}

type snapshot struct {
	Now      time.Time         `json:"now"`
	Hb       float64           `json:"heartbeatSeconds"`
	Nodes    []NodeState       `json:"nodes"`    // one per system (merged from its stations)
	Stations []NodeState       `json:"stations"` // one per container
	Sections []Section         `json:"sections"`
	Alerts   []Alert           `json:"alerts"`
	Events   []model.LogEvent  `json:"events"`
	Tickets  []model.Ticket    `json:"tickets"`
	Discards map[string]int    `json:"discards"`
	Faults   []model.FaultInfo `json:"faults"`
}

// Server is the dashboard service.
type Server struct {
	ctx    context.Context
	hb     time.Duration
	obs    *observer
	pans   store.Pans
	monURL string
	mq     mqtt.Client
	kick   chan struct{}

	mu     sync.Mutex
	logs   logSource
	subs   map[chan []byte]struct{}
	lastEv int64

	lastEvents   []model.LogEvent // what the page shows while SQL Server does not answer
	lastTickets  []model.Ticket
	lastDiscards map[string]int
	sr           scenarioRunner
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// Run starts the dashboard.
func Run() {
	log.SetFlags(log.Ltime | log.Lmicroseconds)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	hb := 5 * time.Second
	if f, err := strconv.ParseFloat(os.Getenv("HEARTBEAT_SECONDS"), 64); err == nil && f > 0 {
		hb = time.Duration(f * float64(time.Second))
	}
	s := &Server{ctx: ctx, hb: hb, monURL: strings.TrimRight(env("MON_URL", "http://localhost:8082"), "/"),
		kick: make(chan struct{}, 1), subs: map[chan []byte]struct{}{}}
	s.obs = newObserver(hb, s.poke)
	s.mq = s.obs.subscribe(env("MQTT_BROKER", "tcp://localhost:1883"))
	go s.obs.watch(ctx.Done())
	go s.openStores()
	go s.pushLoop()
	if secs, err := strconv.ParseFloat(os.Getenv("CREW_SECONDS"), 64); err == nil && secs > 0 {
		go s.crew(time.Duration(secs * float64(time.Second)))
	}
	s.serve(env("HTTP_ADDR", ":8080"))
}

// openStores connects to the databases in the background so the page is up even
// when they are not.
func (s *Server) openStores() {
	pans, err := store.OpenPans(s.ctx)
	if err == nil {
		s.mu.Lock()
		s.pans = pans
		s.mu.Unlock()
	}
	sql, err := store.OpenSQL(s.ctx)
	if err != nil {
		return
	}
	s.mu.Lock()
	s.logs = sql
	s.mu.Unlock()
	log.Printf("ui: reading the event log from SQL Server")
}

func (s *Server) source() logSource {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.logs
}

func (s *Server) panStore() store.Pans {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pans
}

// poke asks the push loop for an immediate snapshot.
func (s *Server) poke() {
	select {
	case s.kick <- struct{}{}:
	default:
	}
}

// ---------------------------------------------------------------- snapshot

func (s *Server) snapshot() snapshot {
	nodes, stations, secs, _ := s.obs.snapshot()
	snap := snapshot{Now: time.Now().UTC(), Hb: s.hb.Seconds(), Nodes: nodes, Stations: stations, Sections: secs,
		Events: []model.LogEvent{}, Tickets: []model.Ticket{}, Discards: map[string]int{}, Faults: model.FaultCatalog}
	var recent []model.LogEvent
	if src := s.source(); src != nil {
		// The three reads run together and wait at most 1.5 s: with SQL Server off the page
		// must still answer at once, so it shows the last values it read.
		ctx, cancel := context.WithTimeout(s.ctx, 1500*time.Millisecond)
		defer cancel()
		var wg sync.WaitGroup
		var ev []model.LogEvent
		var tk []model.Ticket
		var d map[string]int
		var eerr, terr, derr error
		wg.Add(3)
		go func() { defer wg.Done(); ev, eerr = src.Events(ctx, 80) }()
		go func() { defer wg.Done(); tk, terr = src.Tickets(ctx, 30) }()
		go func() { defer wg.Done(); d, derr = src.DiscardCounts(ctx) }()
		wg.Wait()
		s.mu.Lock()
		if eerr == nil {
			s.lastEvents = ev
		}
		if terr == nil && tk != nil {
			s.lastTickets = tk
		}
		if derr == nil && d != nil {
			s.lastDiscards = d
		}
		if s.lastEvents != nil {
			snap.Events = s.lastEvents
		}
		if s.lastTickets != nil {
			snap.Tickets = s.lastTickets
		}
		if s.lastDiscards != nil {
			snap.Discards = s.lastDiscards
		}
		recent = snap.Events
		s.mu.Unlock()
	}
	snap.Alerts = alerts(stations, secs, snap.Tickets, recent)
	return snap
}

// alerts lists what the overseer must look at: open tickets, systems that do not
// answer, an offline monitor, band sections that are not running, recent warnings.
func alerts(nodes []NodeState, secs []Section, tickets []model.Ticket, events []model.LogEvent) []Alert {
	out := []Alert{}
	for _, t := range tickets {
		if t.Status == "open" {
			out = append(out, Alert{Level: "error", Title: "Maintenance needed: " + t.MachineID, Detail: t.Reason, Node: t.Node, At: t.Opened})
		}
	}
	for _, n := range nodes {
		switch {
		case n.ID == model.MON && n.Status == "dead":
			out = append(out, Alert{Level: "error", Title: "Monitor is offline: no new batches are started",
				Detail: "the line finishes the batches it has; no new order or batch is started; events wait for the monitor and arrive when it is back", Node: n.ID, At: time.Now()})
		case n.Status == "silent":
			out = append(out, Alert{Level: "error", Title: n.ID + " is not responding", Detail: "no heartbeat received", Node: n.ID, At: n.LastSeen})
		case n.Status == "dead" && n.ID != model.MON:
			out = append(out, Alert{Level: "error", Title: n.ID + " container is down",
				Detail: "death certificate received; Docker restarts a crashed container, a stopped one needs Start in Components", Node: n.ID, At: time.Now()})
		}
	}
	for _, sec := range secs {
		if sec.State != "run" && sec.State != "off" {
			out = append(out, Alert{Level: "warning", Title: fmt.Sprintf("Band section %d (%s): %s", sec.ID, sec.Name, sec.State), Node: sec.Owner, At: time.Now()})
		}
	}
	cut := time.Now().Add(-45 * time.Second)
	for _, e := range events {
		if e.Severity == model.SevWarning && e.At.After(cut) && len(out) < 14 {
			out = append(out, Alert{Level: "warning", Title: e.Reason, Detail: e.Node + " · " + e.Code, Node: e.Node, At: e.At})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Level == "error" && out[j].Level != "error" })
	return out
}

// ---------------------------------------------------------------- stream

func (s *Server) hasSubs() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.subs) > 0
}

func (s *Server) broadcast(event string, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	msg := []byte("event: " + event + "\ndata: " + string(b) + "\n\n")
	s.mu.Lock()
	defer s.mu.Unlock()
	for c := range s.subs {
		select {
		case c <- msg:
		default:
		}
	}
}

// pushLoop sends a snapshot when something changed (at most every 300 ms) and
// at least once a second, plus an "event" message for every new error event.
func (s *Server) pushLoop() {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	var last time.Time
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-s.kick:
			if time.Since(last) < 300*time.Millisecond {
				continue
			}
		case <-tick.C:
		}
		if !s.hasSubs() {
			continue
		}
		last = time.Now()
		snap := s.snapshot()
		s.broadcast("snapshot", snap)
		for i := len(snap.Events) - 1; i >= 0; i-- { // oldest first
			e := snap.Events[i]
			if e.ID > s.lastEv {
				if s.lastEv != 0 && e.Severity == model.SevError {
					s.broadcast("event", e)
				}
				s.lastEv = e.ID
			}
		}
	}
}

func (s *Server) stream(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", 500)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	ch := make(chan []byte, 64)
	s.mu.Lock()
	s.subs[ch] = struct{}{}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.subs, ch)
		s.mu.Unlock()
	}()
	b, _ := json.Marshal(s.snapshot())
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

// ---------------------------------------------------------------- actions

// inject publishes the fault injection command the systems listen to (NCMD).
func (s *Server) inject(node, code string, count int) error {
	known := false
	for _, n := range model.Nodes {
		known = known || (n == node && n != model.MON)
	}
	if !known {
		return fmt.Errorf("unknown system %q (the monitor has no failure cases of its own)", node)
	}
	if !s.mq.IsConnected() {
		return fmt.Errorf("not connected to the message broker")
	}
	if count <= 0 {
		count = 1
	}
	sg := sig.New(sig.FaultInjection, "code", code, "count", int64(count))
	p := sparkplug.Payload{Timestamp: uint64(time.Now().UnixMilli()), Metrics: sg.Metrics()}
	tok := s.mq.Publish(sparkplug.Topic(model.Group, sparkplug.NCmd, node, ""), 1, false, p.Marshal())
	if !tok.WaitTimeout(3*time.Second) || tok.Error() != nil {
		return fmt.Errorf("publish failed")
	}
	log.Printf("ui: injected %s into %s (x%d)", code, node, count)
	return nil
}

// repair is the Maintenance action: it asks the monitor (Maintenance Manager) to
// confirm the repair of the machine of a ticket.
func (s *Server) repair(ticket int64) error {
	src := s.source()
	if src == nil {
		return fmt.Errorf("no event log yet")
	}
	ctx, cancel := context.WithTimeout(s.ctx, 3*time.Second)
	defer cancel()
	tks, err := src.Tickets(ctx, 200)
	if err != nil {
		return err
	}
	machine := ""
	for _, t := range tks {
		if t.ID == ticket && t.Status == "open" {
			machine = t.MachineID
		}
	}
	if machine == "" {
		return fmt.Errorf("ticket %d is not open", ticket)
	}
	b, _ := json.Marshal(map[string]string{"machineId": machine})
	hc := &http.Client{Timeout: 5 * time.Second}
	r, err := hc.Post(s.monURL+"/api/repair", "application/json", strings.NewReader(string(b)))
	if err != nil {
		return fmt.Errorf("the monitor is offline, a repair cannot be confirmed now (%v)", err)
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		return fmt.Errorf("the monitor refused: %s", r.Status)
	}
	return nil
}

// ---------------------------------------------------------------- http

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) serve(addr string) {
	sub, _ := fs.Sub(webFS, "web")
	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.FS(sub)))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, "ok") })
	mux.HandleFunc("/api/state", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, s.snapshot()) })
	mux.HandleFunc("/api/stream", s.stream)
	mux.HandleFunc("/api/events", func(w http.ResponseWriter, r *http.Request) {
		since, _ := strconv.ParseInt(r.URL.Query().Get("since"), 10, 64)
		out := []model.LogEvent{}
		if src := s.source(); src != nil {
			ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
			defer cancel()
			evs, err := src.Events(ctx, 500)
			if err != nil {
				http.Error(w, err.Error(), 502)
				return
			}
			for i := len(evs) - 1; i >= 0; i-- { // oldest first
				if evs[i].ID > since {
					out = append(out, evs[i])
				}
			}
		}
		writeJSON(w, out)
	})
	mux.HandleFunc("/api/pans", func(w http.ResponseWriter, r *http.Request) {
		ps := s.panStore()
		if ps == nil {
			writeJSON(w, []model.Pan{})
			return
		}
		n := 40
		if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 && v <= 1000 {
			n = v
		}
		pans, err := ps.List(r.Context(), n)
		if err != nil {
			http.Error(w, err.Error(), 502)
			return
		}
		writeJSON(w, pans)
	})
	mux.HandleFunc("/api/pans/summary", func(w http.ResponseWriter, r *http.Request) {
		by, total := map[string]int{}, 0
		if ps := s.panStore(); ps != nil {
			all, err := ps.List(r.Context(), 5000)
			if err != nil {
				http.Error(w, err.Error(), 502)
				return
			}
			for _, p := range all {
				if strings.HasPrefix(p.ID, "PAN-") {
					total++
					by[p.Status]++
				}
			}
		}
		writeJSON(w, map[string]any{"total": total, "byStatus": by})
	})
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
		if err := s.inject(in.Node, in.Code, in.Count); err != nil {
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
		if err := s.repair(in.Ticket); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
	})
	s.registerControl(mux)
	srv := &http.Server{Addr: addr, Handler: mux}
	go func() {
		<-s.ctx.Done()
		srv.Close()
	}()
	log.Printf("dashboard on %s", addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
