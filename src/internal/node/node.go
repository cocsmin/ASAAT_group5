// Package node is the Sparkplug B edge node every service runs on. It owns
// the MQTT connection, the Last Will (NDEATH), birth certificates, sequence
// numbers, the periodic heartbeat and the helper for event signals.
package node

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"

	"cakefactory/internal/model"
	"cakefactory/internal/sig"
	"cakefactory/internal/sparkplug"
)

// Config configures a node.
type Config struct {
	Broker    string        // e.g. tcp://mosquitto:1883
	ID        string        // edge node id, e.g. "THM"
	Heartbeat time.Duration // T_hb
	Host      bool          // primary host application: sees every message
	KeepAlive time.Duration // MQTT keep-alive; the broker declares the node dead after 1.5x
}

// RawHandler receives every decoded Sparkplug message (host nodes only).
type RawHandler func(t sparkplug.ParsedTopic, p sparkplug.Payload)

type sigHandler struct {
	from, name string
	fn         func(from string, s sig.Signal)
}

// Node is a Sparkplug B edge node.
type Node struct {
	cfg   Config
	ctx   context.Context
	c     mqtt.Client
	start time.Time

	mu      sync.Mutex
	seq     uint64
	bdSeq   uint64
	devices map[string]string
	detail  map[string]string
	gauges  map[string]int64

	sigH   []sigHandler
	cmdH   map[string]func(sig.Signal)
	raw    RawHandler
	silent atomic.Bool

	states   map[string]sig.Signal // state signals, republished with every heartbeat
	hostUp   atomic.Bool           // primary host application is online (Sparkplug STATE)
	hostH    []func(bool)
	evSeq    atomic.Uint64
	outMu    sync.Mutex
	outbox   []map[string]any
	outPath  string
	outNudge chan struct{}
	dupMu    sync.Mutex
	dupSeen  map[string]bool
	dupOrder []string
}

// New creates a node. Call Start to connect.
func New(cfg Config) *Node {
	if cfg.Heartbeat <= 0 {
		cfg.Heartbeat = 5 * time.Second
	}
	if cfg.KeepAlive <= 0 {
		cfg.KeepAlive = 5 * time.Second
	}
	n := &Node{
		cfg:      cfg,
		start:    time.Now(),
		bdSeq:    uint64(rand.Intn(256)),
		devices:  map[string]string{},
		detail:   map[string]string{},
		gauges:   map[string]int64{},
		cmdH:     map[string]func(sig.Signal){},
		states:   map[string]sig.Signal{},
		outNudge: make(chan struct{}, 1),
		dupSeen:  map[string]bool{},
	}
	dir := os.Getenv("OUTBOX_DIR")
	if dir == "" {
		dir = "data"
	}
	if os.MkdirAll(dir, 0o755) == nil {
		n.outPath = filepath.Join(dir, "outbox-"+cfg.ID+".json")
		if b, err := os.ReadFile(n.outPath); err == nil {
			_ = json.Unmarshal(b, &n.outbox)
		}
	}
	return n
}

// HostOnline reports whether the primary host application (MON) is online.
// The report requires that production may not run without the monitor.
func (n *Node) HostOnline() bool { return n.cfg.Host || n.hostUp.Load() }

// OnHost registers a callback for changes of the primary host's state.
func (n *Node) OnHost(fn func(online bool)) { n.hostH = append(n.hostH, fn) }

// SetState publishes a state signal now and again with every heartbeat, so a
// restarted receiver learns the current value within one heartbeat interval.
func (n *Node) SetState(s sig.Signal) {
	n.mu.Lock()
	n.states[s.Name] = s
	n.mu.Unlock()
	n.send(1, []sig.Signal{s})
}

// ID returns the edge node id.
func (n *Node) ID() string { return n.cfg.ID }

// OnSignal registers a handler for a signal that another node publishes with
// NDATA. from "" matches any node.
func (n *Node) OnSignal(from, name string, fn func(from string, s sig.Signal)) {
	n.sigH = append(n.sigH, sigHandler{from, name, fn})
}

// OnCommand registers a handler for a command that arrives as NCMD.
func (n *Node) OnCommand(name string, fn func(sig.Signal)) { n.cmdH[name] = fn }

// OnRaw registers the host handler.
func (n *Node) OnRaw(h RawHandler) { n.raw = h }

// SetSilent makes the node stop answering health checks (failure case FC28).
func (n *Node) SetSilent(b bool) { n.silent.Store(b) }

// Silent reports whether the node is currently silent.
func (n *Node) Silent() bool { return n.silent.Load() }

func (n *Node) nextSeq() uint64 {
	n.mu.Lock()
	defer n.mu.Unlock()
	s := n.seq
	n.seq = (n.seq + 1) % 256
	return s
}

func now() uint64 { return uint64(time.Now().UnixMilli()) }

func (n *Node) publish(topic string, p sparkplug.Payload, qos byte, retain bool) {
	if n.c == nil {
		return
	}
	n.c.Publish(topic, qos, retain, p.Marshal())
}

func (n *Node) bdMetric() sparkplug.Metric { return sparkplug.NewMetric("bdSeq", int64(n.bdSeq)) }

// Start connects to the broker, publishes the birth certificates and runs the
// heartbeat until ctx ends. The Last Will is the NDEATH certificate.
func (n *Node) Start(ctx context.Context) error {
	n.ctx = ctx
	will := sparkplug.Payload{Timestamp: now(), Metrics: []sparkplug.Metric{n.bdMetric()}}
	opts := mqtt.NewClientOptions().
		AddBroker(n.cfg.Broker).
		SetClientID("cakefactory-"+strings.ToLower(n.cfg.ID)).
		SetCleanSession(false). // persistent session: the broker queues QoS 1 messages while we are down
		SetAutoReconnect(true).
		SetConnectRetry(true).
		SetConnectRetryInterval(2*time.Second).
		SetKeepAlive(n.cfg.KeepAlive).
		SetPingTimeout(n.cfg.KeepAlive).
		SetWill(sparkplug.Topic(model.Group, sparkplug.NDeath, n.cfg.ID, ""), string(will.Marshal()), 1, false)
	if n.cfg.Host {
		opts.SetWill(sparkplug.StateTopic(n.cfg.ID), `{"online":false}`, 1, true)
	}
	opts.SetOnConnectHandler(func(c mqtt.Client) {
		n.subscribe(c)
		n.birth()
	})
	opts.SetConnectionLostHandler(func(_ mqtt.Client, err error) {
		log.Printf("[%s] mqtt connection lost: %v", n.cfg.ID, err)
	})
	n.c = mqtt.NewClient(opts)
	n.c.Connect().Wait()
	go n.heartbeatLoop(ctx)
	go n.outboxLoop(ctx)
	go func() {
		<-ctx.Done()
		n.shutdown()
	}()
	return nil
}

func (n *Node) shutdown() {
	if n.c == nil || !n.c.IsConnected() {
		return
	}
	p := sparkplug.Payload{Timestamp: now(), Metrics: []sparkplug.Metric{n.bdMetric()}}
	n.c.Publish(sparkplug.Topic(model.Group, sparkplug.NDeath, n.cfg.ID, ""), 1, false, p.Marshal()).WaitTimeout(time.Second)
	n.c.Disconnect(250)
}

func (n *Node) subscribe(c mqtt.Client) {
	g := model.Group
	if n.cfg.Host {
		c.Subscribe(sparkplug.Namespace+"/"+g+"/#", 1, n.onMessage)
	} else {
		c.Subscribe(sparkplug.Topic(g, sparkplug.NData, "+", ""), 1, n.onMessage)
		c.Subscribe(sparkplug.Topic(g, sparkplug.NCmd, n.cfg.ID, ""), 1, n.onMessage)
		c.Subscribe(sparkplug.Namespace+"/STATE/+", 1, n.onMessage)
	}
}

// onMessage must not block: handlers push work onto channels.
func (n *Node) onMessage(_ mqtt.Client, m mqtt.Message) {
	t, ok := sparkplug.ParseTopic(m.Topic())
	if !ok {
		return
	}
	if t.State {
		if n.cfg.Host {
			return
		}
		up := strings.Contains(string(m.Payload()), `"online":true`)
		was := n.hostUp.Swap(up)
		if up && !was {
			n.birth() // primary host (re)started: publish births again
		}
		if up != was {
			for _, h := range n.hostH {
				h(up)
			}
		}
		return
	}
	p, err := sparkplug.Unmarshal(m.Payload())
	if err != nil {
		log.Printf("[%s] bad payload on %s: %v", n.cfg.ID, m.Topic(), err)
		return
	}
	if n.raw != nil {
		n.raw(t, p)
	}
	switch t.Type {
	case sparkplug.NCmd:
		if t.Edge != n.cfg.ID {
			return
		}
		for _, s := range sig.FromMetrics(p.Metrics) {
			if s.Name == sig.NodeControl && s.Bool("Rebirth") {
				n.birth()
				continue
			}
			if fn := n.cmdH[s.Name]; fn != nil {
				fn(s)
			}
		}
	case sparkplug.NData:
		for _, s := range sig.FromMetrics(p.Metrics) {
			if id := s.Str("msgId"); id != "" && n.dup(t.Edge+"/"+id) {
				continue
			}
			for _, h := range n.sigH {
				if h.name == s.Name && (h.from == "" || h.from == t.Edge) && t.Edge != n.cfg.ID {
					h.fn(t.Edge, s)
				}
			}
		}
	}
}

// birth publishes NBIRTH and a DBIRTH for each device.
func (n *Node) birth() {
	n.mu.Lock()
	n.seq = 0
	n.mu.Unlock()
	if n.cfg.Host {
		b, _ := json.Marshal(map[string]any{"online": true, "timestamp": time.Now().UnixMilli()})
		n.c.Publish(sparkplug.StateTopic(n.cfg.ID), 1, true, b)
		return
	}
	ms := []sparkplug.Metric{n.bdMetric(), sparkplug.NewMetric("Node Control/Rebirth", false)}
	ms = append(ms, n.healthMetrics()...)
	p := sparkplug.Payload{Timestamp: now(), Metrics: ms, Seq: n.nextSeq()}
	n.publish(sparkplug.Topic(model.Group, sparkplug.NBirth, n.cfg.ID, ""), p, 0, false)
	n.mu.Lock()
	names := make([]string, 0, len(n.devices))
	for d := range n.devices {
		names = append(names, d)
	}
	n.mu.Unlock()
	sort.Strings(names)
	for _, d := range names {
		n.publishDevice(sparkplug.DBirth, d)
	}
}

func (n *Node) publishDevice(msgType, device string) {
	n.mu.Lock()
	state, detail := n.devices[device], n.detail[device]
	n.mu.Unlock()
	ms := []sparkplug.Metric{sparkplug.NewMetric("State", state), sparkplug.NewMetric("Detail", detail)}
	p := sparkplug.Payload{Timestamp: now(), Metrics: ms, Seq: n.nextSeq()}
	n.publish(sparkplug.Topic(model.Group, msgType, n.cfg.ID, device), p, 0, false)
}

// SetDevice records a device state and publishes DDATA when it changes.
func (n *Node) SetDevice(name, state, detail string) {
	n.mu.Lock()
	old, known := n.devices[name]
	changed := !known || old != state || n.detail[name] != detail
	n.devices[name] = state
	n.detail[name] = detail
	n.mu.Unlock()
	if !changed || n.silent.Load() || n.c == nil {
		return
	}
	if !known {
		n.publishDevice(sparkplug.DBirth, name)
	} else {
		n.publishDevice(sparkplug.DData, name)
	}
}

// Gauge records a numeric value that is published with every heartbeat.
func (n *Node) Gauge(name string, v int64) {
	n.mu.Lock()
	n.gauges[name] = v
	n.mu.Unlock()
}

// AddGauge adds delta to a gauge and returns the new value.
func (n *Node) AddGauge(name string, delta int64) int64 {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.gauges[name] += delta
	return n.gauges[name]
}

// GaugeValue returns a gauge value.
func (n *Node) GaugeValue(name string) int64 {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.gauges[name]
}

// Gauges returns a copy of all gauges (used for state snapshots).
func (n *Node) Gauges() map[string]int64 {
	n.mu.Lock()
	defer n.mu.Unlock()
	out := make(map[string]int64, len(n.gauges))
	for k, v := range n.gauges {
		out[k] = v
	}
	return out
}

// RestoreGauges loads gauges from a snapshot.
func (n *Node) RestoreGauges(g map[string]int64) {
	n.mu.Lock()
	for k, v := range g {
		n.gauges[k] = v
	}
	n.mu.Unlock()
}

func (n *Node) healthMetrics() []sparkplug.Metric {
	n.mu.Lock()
	defer n.mu.Unlock()
	pre := sig.Heartbeat + "/"
	ms := []sparkplug.Metric{sparkplug.NewMetric(pre+"uptime", int64(time.Since(n.start).Seconds()))}
	for d, s := range n.devices {
		ms = append(ms, sparkplug.NewMetric(pre+"dev."+d, s))
	}
	for g, v := range n.gauges {
		ms = append(ms, sparkplug.NewMetric(pre+"g."+g, v))
	}
	return ms
}

func (n *Node) heartbeatLoop(ctx context.Context) {
	t := time.NewTicker(n.cfg.Heartbeat)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if n.silent.Load() || n.c == nil || !n.c.IsConnected() {
				continue
			}
			var ms []sparkplug.Metric
			if !n.cfg.Host {
				ms = n.healthMetrics()
			}
			n.mu.Lock()
			for _, s := range n.states {
				ms = append(ms, s.Metrics()...)
			}
			n.mu.Unlock()
			p := sparkplug.Payload{Timestamp: now(), Metrics: ms, Seq: n.nextSeq()}
			n.publish(sparkplug.Topic(model.Group, sparkplug.NData, n.cfg.ID, ""), p, 0, false)
		}
	}
}

// SendReliable publishes signals with QoS 1 and a message id. The broker queues
// them for a receiver that is down, and the receiver drops duplicates by id.
// Use it for every handoff that must not be lost.
func (n *Node) SendReliable(sigs ...sig.Signal) {
	cp := make([]sig.Signal, len(sigs))
	for i, s := range sigs {
		fl := make(map[string]any, len(s.F)+1)
		for k, v := range s.F {
			fl[k] = v
		}
		fl["msgId"] = fmt.Sprintf("%s-%d-%d", n.cfg.ID, n.start.UnixMilli(), n.evSeq.Add(1))
		cp[i] = sig.Signal{Name: s.Name, F: fl}
	}
	n.send(1, cp)
}

// dup reports whether a message id was seen before.
func (n *Node) dup(key string) bool {
	n.dupMu.Lock()
	defer n.dupMu.Unlock()
	if n.dupSeen[key] {
		return true
	}
	n.dupSeen[key] = true
	n.dupOrder = append(n.dupOrder, key)
	if len(n.dupOrder) > 4000 {
		delete(n.dupSeen, n.dupOrder[0])
		n.dupOrder = n.dupOrder[1:]
	}
	return false
}

// Send publishes signals to every interested node as NDATA.
func (n *Node) Send(sigs ...sig.Signal) { n.send(0, sigs) }

func (n *Node) send(qos byte, sigs []sig.Signal) {
	if n.c == nil {
		return
	}
	var ms []sparkplug.Metric
	for _, s := range sigs {
		ms = append(ms, s.Metrics()...)
	}
	p := sparkplug.Payload{Timestamp: now(), Metrics: ms, Seq: n.nextSeq()}
	n.publish(sparkplug.Topic(model.Group, sparkplug.NData, n.cfg.ID, ""), p, qos, false)
}

// Command sends signals to one node as NCMD.
func (n *Node) Command(target string, sigs ...sig.Signal) {
	if n.c == nil {
		return
	}
	var ms []sparkplug.Metric
	for _, s := range sigs {
		ms = append(ms, s.Metrics()...)
	}
	p := sparkplug.Payload{Timestamp: now(), Metrics: ms}
	n.publish(sparkplug.Topic(model.Group, sparkplug.NCmd, target, ""), p, 1, false)
}

// Event describes one log, warning, error or discard event.
type Event struct {
	Severity    string
	Code        string // use case or failure case, e.g. "FC9"
	Reason      string
	BatchID     string
	Device      string
	MachineID   string // set when Maintenance must repair something
	Maintenance bool
}

// Emit publishes the event signal and mirrors it to the container log.
func (n *Node) Emit(e Event) {
	log.Printf("[%s] %-7s %-5s %s (batch %s)", n.cfg.ID, e.Severity, e.Code, e.Reason, e.BatchID)
	f := map[string]any{
		"eventId":  fmt.Sprintf("%s-%d-%d", n.cfg.ID, n.start.UnixMilli(), n.evSeq.Add(1)),
		"severity": e.Severity, "code": e.Code, "reason": e.Reason, "batchId": e.BatchID,
		"device": e.Device, "machineId": e.MachineID, "maintenance": e.Maintenance,
		"time": time.Now().UTC().Format(time.RFC3339Nano),
	}
	n.outMu.Lock()
	n.outbox = append(n.outbox, f)
	n.persistOutbox()
	n.outMu.Unlock()
	select {
	case n.outNudge <- struct{}{}:
	default:
	}
}

func (n *Node) persistOutbox() {
	if n.outPath == "" {
		return
	}
	b, _ := json.Marshal(n.outbox)
	_ = os.WriteFile(n.outPath+".tmp", b, 0o644)
	_ = os.Rename(n.outPath+".tmp", n.outPath)
}

// outboxLoop delivers events in order with QoS 1 and removes an event only
// after the broker acknowledged it, so no discard event is lost while the bus
// or the monitor is down (THM-REQ-77). The outbox survives a container restart.
func (n *Node) outboxLoop(ctx context.Context) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-n.outNudge:
		case <-t.C:
		}
		for !n.silent.Load() && n.c != nil && n.c.IsConnected() {
			n.outMu.Lock()
			if len(n.outbox) == 0 {
				n.outMu.Unlock()
				break
			}
			head := n.outbox[0]
			n.outMu.Unlock()
			s := sig.Signal{Name: sig.Events, F: head}
			p := sparkplug.Payload{Timestamp: now(), Metrics: s.Metrics(), Seq: n.nextSeq()}
			tok := n.c.Publish(sparkplug.Topic(model.Group, sparkplug.NData, n.cfg.ID, ""), 1, false, p.Marshal())
			if !tok.WaitTimeout(2*time.Second) || tok.Error() != nil {
				break
			}
			n.outMu.Lock()
			n.outbox = n.outbox[1:]
			n.persistOutbox()
			n.outMu.Unlock()
		}
	}
}

// Log, Warn, Err and Discard are shortcuts for Emit.
func (n *Node) Log(code, reason, batch string) {
	n.Emit(Event{Severity: model.SevLog, Code: code, Reason: reason, BatchID: batch})
}

// Warn emits a warning event.
func (n *Node) Warn(code, reason, batch string) {
	n.Emit(Event{Severity: model.SevWarning, Code: code, Reason: reason, BatchID: batch})
}

// Discard emits a discard event.
func (n *Node) Discard(code, reason, batch string) {
	n.Emit(Event{Severity: model.SevDiscard, Code: code, Reason: reason, BatchID: batch})
}

// Fault emits an error event that needs Maintenance for the given machine.
func (n *Node) Fault(code, reason, batch, device, machine string) {
	n.Emit(Event{Severity: model.SevError, Code: code, Reason: reason, BatchID: batch,
		Device: device, MachineID: machine, Maintenance: true})
}
