// Package mon is the Line Monitoring & Health system plus band coordination.
// It is the Sparkplug primary host application: it tracks node presence from
// birth and death certificates, detects silent nodes from missing heartbeats,
// stores every event in SQL, opens maintenance tickets, decides on halt or
// slow-down of band sections and serves the dashboard.
package mon

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"cakefactory/internal/model"
	"cakefactory/internal/node"
	"cakefactory/internal/sig"
	"cakefactory/internal/sim"
	"cakefactory/internal/sparkplug"
	"cakefactory/internal/store"
	"cakefactory/internal/svc"
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

// Alert is something the production overseer should look at.
type Alert struct {
	Level  string    `json:"level"` // warning | error
	Title  string    `json:"title"`
	Detail string    `json:"detail"`
	Node   string    `json:"node"`
	At     time.Time `json:"at"`
}

// Monitor is the line monitoring and health service.
type Monitor struct {
	*svc.Base
	sql       store.SQL
	hb        time.Duration
	work      chan func()
	mu        sync.Mutex
	nodes     map[string]*NodeState
	sections  [6]Section
	events    []model.LogEvent
	tickets   map[string]model.Ticket // open tickets by machine id
	discards  map[string]int          // discards by node
	silent    map[string]bool         // nodes handled as silent (FC28)
	subs      map[chan []byte]struct{}
	warnings  []Alert
	haltNodes map[string]bool
	seenEv    map[string]bool
	seenOrder []string // nodes whose sections are halted because they went silent
}

// Run starts the service.
func Run() {
	b, cancel := svc.Run(model.MON)
	defer cancel()
	sqlStore, err := store.OpenSQL(b.Ctx)
	if err != nil {
		log.Fatal(err)
	}
	m := &Monitor{Base: b, sql: sqlStore, hb: svc.EnvDur("HEARTBEAT_SECONDS", 5*time.Second),
		work: make(chan func(), 2048), nodes: map[string]*NodeState{}, tickets: map[string]model.Ticket{},
		discards: map[string]int{}, silent: map[string]bool{}, subs: map[chan []byte]struct{}{},
		haltNodes: map[string]bool{}, seenEv: map[string]bool{}}
	for _, id := range model.Nodes {
		if id != model.MON {
			m.nodes[id] = &NodeState{ID: id, Status: "unknown", Devices: map[string]DevState{}, Gauges: map[string]int64{}}
		}
	}
	names := [6]string{"", "Depositing", "Baking tunnel", "Cooling", "Depanning & inspection", "Packaging"}
	owners := [6]string{"", model.THM, model.THM, model.THM, model.FIN, model.FIN}
	for i := 1; i <= 5; i++ {
		m.sections[i] = Section{ID: i, Name: names[i], Owner: owners[i], State: "run"}
	}
	m.loadHistory()
	m.N.OnRaw(func(t sparkplug.ParsedTopic, p sparkplug.Payload) {
		m.work <- func() { m.onMessage(t, p) }
	})
	m.N.OnCommand(sig.FaultInjection, func(s sig.Signal) { m.injectLocal(s.Str("code")) })
	m.Start()
	go m.worker()
	go m.watchdog()
	go m.ticker()
	m.serve(svc.Env("HTTP_ADDR", ":8080"))
}

func (m *Monitor) loadHistory() {
	if evs, err := m.sql.Events(m.Ctx, 120); err == nil {
		for i := len(evs) - 1; i >= 0; i-- {
			m.events = append(m.events, evs[i])
			if evs[i].Severity == model.SevDiscard {
				m.discards[evs[i].Node]++
			}
		}
	}
	if ts, err := m.sql.Tickets(m.Ctx, 200); err == nil {
		for _, t := range ts {
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
		m.restoreNodeSections(n.ID)
	}
	m.broadcastBand()
	m.push()
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
	m.push()
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
	m.push()
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
		limit := 3 * m.hb
		var gone []*NodeState
		m.mu.Lock()
		for _, n := range m.nodes {
			if n.Status == "online" && !n.LastSeen.IsZero() && time.Since(n.LastSeen) > limit {
				n.Status, n.Note = "silent", "no heartbeat"
				m.silent[n.ID] = true
				gone = append(gone, n)
			}
			if n.Status == "dead" && time.Since(n.deadAt) > 45*time.Second && !m.hasTicket(strings.ToLower(n.ID)+"-container") {
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
		m.push()
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
		_ = m.sql.CloseTicket(m.Ctx, t.ID)
	}
}

func (m *Monitor) hasTicket(machine string) bool {
	_, ok := m.tickets[machine]
	return ok
}

func (m *Monitor) ticker() {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-m.Ctx.Done():
			return
		case <-t.C:
			m.push()
		}
	}
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
	if err := m.sql.InsertEvent(m.Ctx, &ev); err != nil {
		log.Printf("event log: %v", err)
	}
	m.mu.Lock()
	m.events = append(m.events, ev)
	if len(m.events) > 400 {
		m.events = m.events[len(m.events)-400:]
	}
	switch ev.Severity {
	case model.SevDiscard:
		m.discards[from]++
	case model.SevWarning:
		m.warnings = append(m.warnings, Alert{Level: "warning", Title: ev.Reason, Detail: from + " · " + ev.Code, Node: from, At: time.Now()})
	}
	m.mu.Unlock()
	if ev.Severity == model.SevError && ev.Maintenance && ev.MachineID != "" {
		m.openTicket(ev)
	}
	m.pushEvent(ev)
}

func (m *Monitor) openTicket(ev model.LogEvent) {
	m.mu.Lock()
	if _, open := m.tickets[ev.MachineID]; open {
		m.mu.Unlock()
		return
	}
	m.mu.Unlock()
	t := model.Ticket{Node: nodeOf(ev.MachineID, ev.Node), MachineID: ev.MachineID, Reason: ev.Reason}
	if err := m.sql.OpenTicket(m.Ctx, &t); err != nil {
		log.Printf("ticket: %v", err)
		return
	}
	m.mu.Lock()
	m.tickets[ev.MachineID] = t
	m.mu.Unlock()
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
	if err := m.sql.CloseTicket(m.Ctx, ticketID); err != nil {
		return err
	}
	m.mu.Lock()
	delete(m.tickets, tk.MachineID)
	m.mu.Unlock()
	m.N.Log("UC2", fmt.Sprintf("Maintenance repaired %s (ticket %d): repair confirmed", tk.MachineID, tk.ID), "")
	if strings.HasPrefix(tk.MachineID, "mon-section-") {
		var id int
		fmt.Sscanf(tk.MachineID, "mon-section-%d", &id)
		go m.sectionRepaired(id)
	} else if strings.HasSuffix(tk.MachineID, "-container") {
		// nothing to command: the container must be started by an operator
	} else {
		m.N.Command(tk.Node, sig.New(sig.RepairConfirmed, "machineId", tk.MachineID, "repaired", true))
	}
	m.push()
	return nil
}

// Inject arms a failure case on a node.
func (m *Monitor) Inject(node, code string, count int) error {
	if node == model.MON {
		m.injectLocal(code)
		return nil
	}
	if _, ok := m.nodes[node]; !ok {
		return fmt.Errorf("unknown node %q", node)
	}
	m.N.Command(node, sig.New(sig.FaultInjection, "code", code, "count", int64(count)))
	m.N.Log("UC2", fmt.Sprintf("failure case %s injected into %s from the dashboard", code, node), "")
	return nil
}

// --------------------------------------------------------------- band

// injectLocal handles the failure cases of the band itself.
func (m *Monitor) injectLocal(code string) {
	sec := int(time.Now().UnixNano()/1000)%5 + 1
	switch code {
	case "FC19":
		go m.sectionStops(sec)
	case "FC20":
		go m.wrongSpeed(sec)
	}
}

func (m *Monitor) setSection(i int, state string) {
	m.mu.Lock()
	m.sections[i].State = state
	m.mu.Unlock()
}

// sectionStops: the stopped section halts everything upstream; downstream
// sections run until empty (FC19).
// reportHalt follows the UC2 error flow: the monitor reports its halt (or
// slow-down) intent to the Transport/Conveyor system and then reports the
// production halt (or slow-down) to the overseer, who sees it on the dashboard.
func (m *Monitor) reportHalt(kind, why string) {
	m.N.Log("UC2", "halt intent reported to the band: "+why, "")
	if kind == "slow" {
		m.N.Warn("UC2", "production slow-down reported to the overseer: "+why, "")
		return
	}
	m.N.Warn("UC2", "production halt reported to the overseer: "+why, "")
}

func (m *Monitor) sectionStops(i int) {
	m.reportHalt("halt", fmt.Sprintf("section %d (%s) stopped", i, m.sections[i].Name))
	m.setSection(i, "fault")
	for u := 1; u < i; u++ {
		m.setSection(u, "halt")
	}
	m.broadcastBand()
	m.N.Emit(evt(model.SevError, "FC19", fmt.Sprintf("band section %d (%s) stopped: upstream sections halted, downstream run until empty", i, m.sections[i].Name),
		m.sections[i].Owner, fmt.Sprintf("mon-section-%d", i)))
	m.push()
}

func (m *Monitor) sectionRepaired(i int) {
	m.setSection(i, "run")
	m.broadcastBand()
	m.N.Log("FC19", fmt.Sprintf("section %d restarted; upstream sections restart in order", i), "")
	for u := i - 1; u >= 1; u-- {
		if !sim.Sleep(m.Ctx, 1500*time.Millisecond) {
			return
		}
		m.setSection(u, "run")
		m.broadcastBand()
	}
	m.N.Log("FC19", "all band sections running again", "")
	m.push()
}

func (m *Monitor) wrongSpeed(i int) {
	m.reportHalt("slow", fmt.Sprintf("section %d runs at the wrong speed", i))
	m.setSection(i, "slow")
	m.broadcastBand()
	m.N.Warn("FC20", fmt.Sprintf("section %d runs at the wrong speed: speed correction requested", i), "")
	if !sim.Sleep(m.Ctx, 10*time.Second) {
		return
	}
	m.setSection(i, "run")
	m.broadcastBand()
	m.N.Log("FC20", fmt.Sprintf("section %d speed corrected", i), "")
	m.push()
}

// haltNodeSections halts or restores the sections of a node that went silent or died.
func (m *Monitor) haltNodeSections(node string, halt bool) {
	if halt {
		m.reportHalt("halt", node+" is not responding or its container is down")
	}
	m.mu.Lock()
	for i := 1; i <= 5; i++ {
		if m.sections[i].Owner != node {
			continue
		}
		switch {
		case halt && m.sections[i].State == "run":
			m.sections[i].State = "halt"
		case !halt && m.sections[i].State == "halt":
			m.sections[i].State = "run"
		}
	}
	m.mu.Unlock()
	m.broadcastBand()
}

func (m *Monitor) restoreNodeSections(node string) { m.haltNodeSections(node, false) }

// broadcastBand sends the Band state change signal to THM and FIN.
func (m *Monitor) broadcastBand() {
	m.mu.Lock()
	kv := []any{}
	for i := 1; i <= 5; i++ {
		st := m.sections[i].State
		if st == "fault" {
			st = "halt"
		}
		kv = append(kv, fmt.Sprintf("S%d", i), st)
	}
	m.mu.Unlock()
	s := sig.New(sig.BandState, kv...)
	m.N.SetState(s) // THM and FIN receive it; it is repeated with every heartbeat
}

// evt builds an error event that the monitor raises on behalf of a node.
func evt(sev, code, reason, device, machine string) node.Event {
	return node.Event{Severity: sev, Code: code, Reason: reason, Device: device, MachineID: machine, Maintenance: machine != ""}
}

// nodeOf derives the owning node from a machine id such as thm-zone-2.
func nodeOf(machine, fallback string) string {
	prefix, _, _ := strings.Cut(machine, "-")
	if p := strings.ToUpper(prefix); p == model.PLM || p == model.BAT || p == model.THM || p == model.FIN || p == model.MON {
		return p
	}
	return fallback
}
