// Package mon is the Line Monitoring & Health system plus band coordination.
// It is the Sparkplug primary host application: it tracks node presence from
// birth and death certificates, detects silent nodes from missing heartbeats,
// stores every event in SQL, opens maintenance tickets and decides on halt or
// slow-down of band sections. It has no user interface: the experiment dashboard
// is the separate ui service. MON only answers a small read and repair API.
package mon

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"cakefactory/internal/model"
	"cakefactory/internal/node"
	"cakefactory/internal/sig"
	"cakefactory/internal/sparkplug"
	"cakefactory/internal/store"
	"cakefactory/internal/svc"
	"cakefactory/internal/timing"
)

// DevState is the last reported state of one device.
type DevState struct {
	State  string `json:"state"`
	Detail string `json:"detail"`
}

// NodeState is what the monitor knows about one edge node.
type NodeState struct {
	ID       string              `json:"id"`
	Status   string              `json:"status"` // unknown | online | silent | dead
	LastSeen time.Time           `json:"lastSeen"`
	Restarts int                 `json:"restarts"`
	Uptime   int64               `json:"uptime"`
	Note     string              `json:"note"`
	Devices  map[string]DevState `json:"devices"`
	Gauges   map[string]int64    `json:"gauges"`
	born     bool
	bdSeq    int64
	deadAt   time.Time
}

// Section is one independently drivable band section.
type Section struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Owner string `json:"owner"`
	State string `json:"state"` // run | slow | halt | fault
}

// Monitor is the line monitoring and health service.
type Monitor struct {
	*svc.Base
	sql           store.SQL
	hb            time.Duration
	work          chan func()
	mu            sync.Mutex
	nodes         map[string]*NodeState
	sections      [6]Section
	events        []model.LogEvent
	tickets       map[string]model.Ticket // open tickets by machine id
	discards      map[string]int          // discards by node
	silent        map[string]bool         // nodes handled as silent (FC28)
	haltNodes     map[string]bool
	seenEv        map[string]bool
	dbq           chan func() bool
	evID          atomic.Int64
	tkID          atomic.Int64
	sqlTicket     map[int64]int64 // ticket id in memory -> row id in SQL
	recentTickets []model.Ticket
	seenOrder     []string // nodes whose sections are halted because they went silent
}

// Run starts the service.
func Run() {
	b, cancel := svc.Run(model.MON)
	defer cancel()
	sqlStore, err := store.OpenSQL(b.Ctx)
	if err != nil {
		log.Fatal(err)
	}
	m := &Monitor{Base: b, sql: sqlStore, hb: svc.EnvDur("HEARTBEAT_SECONDS", timing.Heartbeat),
		work: make(chan func(), 2048), nodes: map[string]*NodeState{}, tickets: map[string]model.Ticket{},
		discards: map[string]int{}, silent: map[string]bool{},
		haltNodes: map[string]bool{}, seenEv: map[string]bool{},
		dbq: make(chan func() bool, 20000), sqlTicket: map[int64]int64{}}
	for _, id := range model.Nodes {
		if id != model.MON {
			m.nodes[id] = &NodeState{ID: id, Status: "unknown", Devices: map[string]DevState{}, Gauges: map[string]int64{}}
		}
	}
	for i := 1; i <= 5; i++ { // the state comes from Band Transport ("Band state change")
		m.sections[i] = Section{ID: i, Name: model.SectionName(i), Owner: model.SectionOwner(i), State: "run"}
	}
	m.loadHistory()
	m.startDB()
	m.N.OnRaw(func(t sparkplug.ParsedTopic, p sparkplug.Payload) {
		if isHealth(t, p) {
			m.touch(t.Edge)
		}
		m.work <- func() { m.onMessage(t, p) }
	})
	m.Start()
	go m.worker()
	go m.watchdog()
	m.serve(svc.Env("HTTP_ADDR", ":8082"))
}

func (m *Monitor) loadHistory() {
	c, cancel := context.WithTimeout(m.Ctx, 5*time.Second)
	defer cancel()
	if evs, err := m.sql.Events(c, 120); err == nil {
		for i := len(evs) - 1; i >= 0; i-- {
			m.events = append(m.events, evs[i])
			if evs[i].ID > m.evID.Load() {
				m.evID.Store(evs[i].ID)
			}
			if evs[i].Severity == model.SevDiscard {
				m.discards[evs[i].Node]++
			}
		}
	}
	if ts, err := m.sql.Tickets(c, 200); err == nil {
		m.recentTickets = ts
		for _, t := range ts {
			m.sqlTicket[t.ID] = t.ID
			if t.ID > m.tkID.Load() {
				m.tkID.Store(t.ID)
			}
			if t.Status == "open" {
				m.tickets[t.MachineID] = t
			}
		}
	}
}

func (m *Monitor) worker() {
	for {
		select {
		case <-m.Ctx.Done():
			return
		case f := <-m.work:
			f()
		}
	}
}

func (m *Monitor) onMessage(t sparkplug.ParsedTopic, p sparkplug.Payload) {
	if t.Edge == model.MON && t.Type != sparkplug.NData {
		return
	}
	m.mu.Lock()
	n := m.nodes[t.Edge]
	m.mu.Unlock()
	switch t.Type {
	case sparkplug.NBirth:
		if n != nil {
			m.onBirth(n, p)
		}
	case sparkplug.NDeath:
		if n != nil {
			m.onDeath(n)
		}
	case sparkplug.DBirth, sparkplug.DData:
		if n != nil {
			m.onDevice(n, t.Device, p)
		}
	case sparkplug.NData:
		if t.Edge == model.MON {
			m.onSignals(t.Edge, p)
			return
		}
		if n == nil {
			return
		}
		m.mu.Lock()
		known := n.born
		m.mu.Unlock()
		if !known { // we missed the birth certificate: ask for a rebirth
			m.N.Command(t.Edge, sig.New(sig.NodeControl, "Rebirth", true))
		}
		m.onSignals(t.Edge, p)
	}
}

func (m *Monitor) onBirth(n *NodeState, p sparkplug.Payload) {
	var bd int64
	for _, mt := range p.Metrics {
		if mt.Name == "bdSeq" {
			bd, _ = mt.Value.(int64)
		}
	}
	m.mu.Lock()
	restarted := n.born && (bd != n.bdSeq || n.Status == "dead")
	n.bdSeq = bd
	n.born = true
	n.Status, n.LastSeen, n.Note = "online", time.Now(), ""
	if restarted && !m.silent[n.ID] {
		n.Restarts++
	}
	wasSilent := m.silent[n.ID]
	m.mu.Unlock()
	m.absorbHealth(n, p.Metrics)
	m.closeContainerTicket(n.ID)
	if restarted && !wasSilent {
		m.N.Log("FC27", n.ID+" container is back: birth certificate received, last saved state loaded", "")
		m.haltNodeSections(n.ID, false)
	}
	if n.ID == model.BT { // a restarted Band Transport forgets the commands: repeat them
		m.resendCommands()
	}
}

func (m *Monitor) onDeath(n *NodeState) {
	m.mu.Lock()
	n.Status, n.deadAt = "dead", time.Now()
	n.Note = "death certificate (Last Will) received"
	for k := range n.Devices {
		n.Devices[k] = DevState{State: model.StateOffline, Detail: "node dead"}
	}
	m.mu.Unlock()
	m.N.Warn("FC27", n.ID+" container died (NDEATH): Docker restarts it and it reloads its last state", "")
	m.haltNodeSections(n.ID, true)
}

func (m *Monitor) onDevice(n *NodeState, dev string, p sparkplug.Payload) {
	var st DevState
	for _, mt := range p.Metrics {
		switch mt.Name {
		case "State":
			st.State, _ = mt.Value.(string)
		case "Detail":
			st.Detail, _ = mt.Value.(string)
		}
	}
	m.mu.Lock()
	n.Devices[dev] = st
	n.LastSeen = time.Now()
	m.mu.Unlock()
}

// absorbHealth stores the heartbeat metrics of a node.
func (m *Monitor) absorbHealth(n *NodeState, ms []sparkplug.Metric) {
	pre := sig.Heartbeat + "/"
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, mt := range ms {
		name, ok := strings.CutPrefix(mt.Name, pre)
		if !ok {
			continue
		}
		switch {
		case name == "uptime":
			if v, ok := mt.Value.(int64); ok {
				n.Uptime = v
			}
		case strings.HasPrefix(name, "dev."):
			d := strings.TrimPrefix(name, "dev.")
			cur := n.Devices[d]
			cur.State, _ = mt.Value.(string)
			n.Devices[d] = cur
		case strings.HasPrefix(name, "g."):
			if v, ok := mt.Value.(int64); ok {
				n.Gauges[strings.TrimPrefix(name, "g.")] = v
			}
		}
	}
}

func (m *Monitor) onSignals(from string, p sparkplug.Payload) {
	for _, s := range sig.FromMetrics(p.Metrics) {
		switch s.Name {
		case sig.Heartbeat:
			m.mu.Lock()
			n := m.nodes[from]
			m.mu.Unlock()
			if n != nil {
				m.onHeartbeat(n, p)
			}
		case sig.Events:
			m.handleEvent(from, s)
		case sig.BandState:
			if from == model.BT {
				m.onBand(s)
			}
		}
	}
}

func (m *Monitor) onHeartbeat(n *NodeState, p sparkplug.Payload) {
	m.absorbHealth(n, p.Metrics)
	m.mu.Lock()
	n.LastSeen = time.Now()
	wasSilent := n.Status == "silent"
	if n.Status != "online" {
		n.Status, n.Note = "online", ""
	}
	delete(m.silent, n.ID)
	m.mu.Unlock()
	if wasSilent {
		m.N.Log("FC28", n.ID+" answers again: machine marked operational", "")
		m.haltNodeSections(n.ID, false)
	}
}

// watchdog detects nodes that stopped sending heartbeats (UC2, FC28).
func (m *Monitor) watchdog() {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-m.Ctx.Done():
			return
		case <-t.C:
		}
		limit := timing.WatchdogMisses * m.hb
		var gone []*NodeState
		m.mu.Lock()
		for _, n := range m.nodes {
			if n.Status == "online" && !n.LastSeen.IsZero() && time.Since(n.LastSeen) > limit {
				n.Status, n.Note = "silent", "no heartbeat"
				m.silent[n.ID] = true
				gone = append(gone, n)
			}
			if n.Status == "dead" && time.Since(n.deadAt) > timing.MonDeadAfter && !m.hasTicket(strings.ToLower(n.ID)+"-container") {
				gone = append(gone, n)
			}
		}
		m.mu.Unlock()
		for _, n := range gone {
			if n.Status == "silent" {
				m.N.Emit(evt(model.SevError, "FC28", n.ID+" stopped sending status: marked faulty, Maintenance notified",
					n.ID, strings.ToLower(n.ID)+"-controller"))
				m.haltNodeSections(n.ID, true)
			} else {
				m.N.Emit(evt(model.SevError, "FC27", n.ID+" container has not restarted in 45 s",
					n.ID, strings.ToLower(n.ID)+"-container"))
			}
		}
	}
}

// closeContainerTicket closes the ticket of a container that did not restart
// by itself once its birth certificate arrives: nobody has to confirm that.
func (m *Monitor) closeContainerTicket(node string) {
	machine := strings.ToLower(node) + "-container"
	m.mu.Lock()
	t, ok := m.tickets[machine]
	delete(m.tickets, machine)
	m.mu.Unlock()
	if ok {
		m.closeTicketRecord(t.ID)
	}
}

func (m *Monitor) hasTicket(machine string) bool {
	_, ok := m.tickets[machine]
	return ok
}

// ---------------------------------------------------------------- events

func (m *Monitor) handleEvent(from string, s sig.Signal) {
	if id := s.Str("eventId"); id != "" { // events are delivered at least once: drop duplicates
		if m.seenEv[id] {
			return
		}
		m.seenEv[id] = true
		m.seenOrder = append(m.seenOrder, id)
		if len(m.seenOrder) > 4000 {
			delete(m.seenEv, m.seenOrder[0])
			m.seenOrder = m.seenOrder[1:]
		}
	}
	at, err := time.Parse(time.RFC3339Nano, s.Str("time"))
	if err != nil {
		at = time.Now().UTC()
	}
	ev := model.LogEvent{
		At: at, Node: from, Device: s.Str("device"), Severity: s.Str("severity"), Code: s.Str("code"),
		Reason: s.Str("reason"), BatchID: s.Str("batchId"), MachineID: s.Str("machineId"), Maintenance: s.Bool("maintenance"),
	}
	ev.ID = m.evID.Add(1)
	m.persistEvent(ev)
	m.mu.Lock()
	m.events = append(m.events, ev)
	if len(m.events) > 400 {
		m.events = m.events[len(m.events)-400:]
	}
	switch ev.Severity {
	case model.SevDiscard:
		m.discards[from]++
	}
	m.mu.Unlock()
	if ev.Severity == model.SevError && ev.Maintenance && ev.MachineID != "" {
		m.openTicket(ev)
	}
}

func (m *Monitor) openTicket(ev model.LogEvent) {
	m.mu.Lock()
	if _, open := m.tickets[ev.MachineID]; open {
		m.mu.Unlock()
		return
	}
	m.mu.Unlock()
	t := model.Ticket{ID: m.tkID.Add(1), Opened: time.Now().UTC(), Status: "open",
		Node: nodeOf(ev.MachineID, ev.Node), MachineID: ev.MachineID, Reason: ev.Reason}
	m.mu.Lock()
	m.tickets[ev.MachineID] = t
	m.recentTickets = append([]model.Ticket{t}, m.recentTickets...)
	if len(m.recentTickets) > 60 {
		m.recentTickets = m.recentTickets[:60]
	}
	m.mu.Unlock()
	m.dbDo(func() bool {
		c, cancel := m.dbCtx()
		defer cancel()
		x := t
		if err := m.sql.OpenTicket(c, &x); err != nil {
			warnDB("monitor: database not available for tickets (will retry): %v", err)
			return false
		}
		m.mu.Lock()
		m.sqlTicket[t.ID] = x.ID
		m.mu.Unlock()
		return true
	})
}

// Repair is called when Maintenance confirms a repair (dashboard button).
func (m *Monitor) Repair(ticketID int64) error {
	m.mu.Lock()
	var tk model.Ticket
	found := false
	for _, t := range m.tickets {
		if t.ID == ticketID {
			tk, found = t, true
		}
	}
	m.mu.Unlock()
	if !found {
		return fmt.Errorf("ticket %d is not open", ticketID)
	}
	m.closeTicketRecord(ticketID)
	m.mu.Lock()
	delete(m.tickets, tk.MachineID)
	m.mu.Unlock()
	m.N.Log("UC2", fmt.Sprintf("Maintenance repaired %s (ticket %d): repair confirmed", tk.MachineID, tk.ID), "")
	if strings.HasSuffix(tk.MachineID, "-container") {
		// nothing to command: the container must be started by an operator
	} else {
		m.N.Command(tk.Node, sig.New(sig.RepairConfirmed, "machineId", tk.MachineID, "repaired", true))
	}
	return nil
}

// RepairMachine confirms the repair of the open ticket of a machine.
func (m *Monitor) RepairMachine(machine string) error {
	m.mu.Lock()
	t, ok := m.tickets[machine]
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("no open ticket for %s", machine)
	}
	return m.Repair(t.ID)
}

// Inject arms a failure case on a node.
func (m *Monitor) Inject(node, code string, count int) error {
	if node == model.MON {
		return fmt.Errorf("the monitor has no failure cases of its own; the band ones (FC19, FC20) belong to BT")
	}
	if _, ok := m.nodes[node]; !ok {
		return fmt.Errorf("unknown node %q", node)
	}
	m.N.Command(node, sig.New(sig.FaultInjection, "code", code, "count", int64(count)))
	m.N.Log("UC2", fmt.Sprintf("failure case %s injected into %s from the dashboard", code, node), "")
	return nil
}

// --------------------------------------------------------------- band

// The band sections belong to the Band Transport system. The monitor shows what BT
// reports ("Band state change") and, following the UC2 error flow, tells BT to halt
// or restart the sections of a system that went silent or died.

// onBand stores the state of the band sections that BT reports (D1..D5 carry the
// detail, including "fault").
func (m *Monitor) onBand(s sig.Signal) {
	m.mu.Lock()
	for i := 1; i <= 5; i++ {
		d := s.Str(fmt.Sprintf("D%d", i))
		if d == "" {
			d = s.Str(fmt.Sprintf("S%d", i))
		}
		if d != "" {
			m.sections[i].State = d
		}
	}
	m.mu.Unlock()
}

// reportHalt is the monitor's part of the UC2 error flow: it reports its halt
// intent to the Transport/Conveyor system. Band Transport halts and reports the
// production halt (or slow-down) to the overseer.
func (m *Monitor) reportHalt(why string) {
	m.N.Log("UC2", "halt intent reported to the band: "+why, "")
}

// haltNodeSections asks BT to halt, or to restart, the sections of a system that
// went silent or died.
func (m *Monitor) haltNodeSections(node string, halt bool) {
	owns := false
	for i := 1; i <= 5; i++ {
		owns = owns || model.SectionOwner(i) == node
	}
	if !owns {
		return // PLM, BAT, BT and MON work on no band section
	}
	m.mu.Lock()
	was := m.haltNodes[node]
	m.haltNodes[node] = halt
	m.mu.Unlock()
	if halt && !was {
		m.reportHalt(node + " is not responding or its container is down")
	}
	if halt == was && !halt {
		return // nothing was halted: no command needed
	}
	state, why := "run", node+" is back"
	if halt {
		state, why = "halt", node+" is not responding or its container is down"
	}
	m.N.Command(model.BT, sig.New(sig.SectionCommand, "node", node, "state", state, "reason", why))
}

// resendCommands repeats the halts that are still in force, for a Band Transport
// that has just restarted.
func (m *Monitor) resendCommands() {
	m.mu.Lock()
	var nodes []string
	for n, h := range m.haltNodes {
		if h {
			nodes = append(nodes, n)
		}
	}
	m.mu.Unlock()
	for _, n := range nodes {
		m.N.Command(model.BT, sig.New(sig.SectionCommand, "node", n, "state", "halt", "reason", n+" is still not responding"))
	}
}

// evt builds an error event that the monitor raises on behalf of a node.
func evt(sev, code, reason, device, machine string) node.Event {
	return node.Event{Severity: sev, Code: code, Reason: reason, Device: device, MachineID: machine, Maintenance: machine != ""}
}

// nodeOf derives the owning node from a machine id such as thm-zone-2.
func nodeOf(machine, fallback string) string {
	prefix, _, _ := strings.Cut(machine, "-")
	if p := strings.ToUpper(prefix); p == model.PLM || p == model.BAT || p == model.THM || p == model.FIN || p == model.BT || p == model.MON {
		return p
	}
	return fallback
}
