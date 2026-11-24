package ui

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"

	"cakefactory/internal/model"
	"cakefactory/internal/sig"
	"cakefactory/internal/sparkplug"
)

// DevState is the last reported state of one device.
type DevState struct {
	State  string `json:"state"`
	Detail string `json:"detail"`
}

// NodeState is what the observer has seen of one edge node.
type NodeState struct {
	ID       string              `json:"id"`
	Status   string              `json:"status"` // unknown | online | silent | dead
	LastSeen time.Time           `json:"lastSeen"`
	Restarts int                 `json:"restarts"`
	Uptime   int64               `json:"uptime"`
	Note     string              `json:"note"`
	Stations []string            `json:"stations,omitempty"` // the containers of a system that is split per station
	Devices  map[string]DevState `json:"devices"`
	Gauges   map[string]int64    `json:"gauges"`
	born     bool
	bdSeq    int64
}

// Section is one band section as Band Transport reports it.
type Section struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Owner string `json:"owner"`
	State string `json:"state"` // run | slow | halt | fault | off
}

// observer is a passive Sparkplug B subscriber. It is not a host application and
// publishes nothing: it only listens to births, deaths, device data, heartbeats
// and the band state, so it keeps working, and shows the outage, when MON is down.
type observer struct {
	hb       time.Duration
	mu       sync.Mutex
	nodes    map[string]*NodeState
	sections [6]Section
	hostUp   bool
	hostSeen bool
	kick     func()
}

func newObserver(hb time.Duration, kick func()) *observer {
	o := &observer{hb: hb, nodes: map[string]*NodeState{}, kick: kick}
	for _, id := range model.Nodes {
		if id == model.MON {
			continue
		}
		o.nodes[id] = &NodeState{ID: id, Status: "unknown", Devices: map[string]DevState{}, Gauges: map[string]int64{}}
	}
	for i := 1; i <= 5; i++ {
		o.sections[i] = Section{ID: i, Name: model.SectionName(i), Owner: model.SectionOwner(i), State: "run"}
	}
	return o
}

// subscribe connects a read-only MQTT client to the factory's topics.
func (o *observer) subscribe(broker string) mqtt.Client {
	opts := mqtt.NewClientOptions().AddBroker(broker).SetClientID("cakefactory-ui").
		SetCleanSession(true).SetAutoReconnect(true).SetConnectRetry(true).
		SetConnectRetryInterval(2 * time.Second).SetKeepAlive(15 * time.Second)
	opts.SetOnConnectHandler(func(c mqtt.Client) {
		c.Subscribe(sparkplug.Namespace+"/"+model.Group+"/#", 0, o.onMessage)
		c.Subscribe(sparkplug.Namespace+"/STATE/+", 0, o.onMessage)
	})
	c := mqtt.NewClient(opts)
	c.Connect() // retries in the background
	return c
}

func (o *observer) onMessage(_ mqtt.Client, m mqtt.Message) {
	t, ok := sparkplug.ParseTopic(m.Topic())
	if !ok {
		return
	}
	if t.State {
		var st struct {
			Online bool `json:"online"`
		}
		if json.Unmarshal(m.Payload(), &st) == nil {
			o.mu.Lock()
			o.hostUp, o.hostSeen = st.Online, true
			o.mu.Unlock()
			o.kick()
		}
		return
	}
	p, err := sparkplug.Unmarshal(m.Payload())
	if err != nil {
		log.Printf("ui: bad payload on %s: %v", m.Topic(), err)
		return
	}
	o.mu.Lock()
	n := o.nodes[t.Edge]
	if n == nil {
		o.mu.Unlock()
		return
	}
	switch t.Type {
	case sparkplug.NBirth:
		var bd int64
		for _, mt := range p.Metrics {
			if mt.Name == "bdSeq" {
				bd, _ = mt.Value.(int64)
			}
		}
		if n.born && (bd != n.bdSeq || n.Status == "dead") {
			n.Restarts++
		}
		n.bdSeq, n.born = bd, true
		n.Status, n.LastSeen, n.Note = "online", time.Now(), ""
		o.absorb(n, p.Metrics)
	case sparkplug.NDeath:
		n.Status = "dead"
		n.Note = "death certificate (Last Will) received"
		for k := range n.Devices {
			n.Devices[k] = DevState{State: model.StateOffline, Detail: "node dead"}
		}
	case sparkplug.DBirth, sparkplug.DData:
		var st DevState
		for _, mt := range p.Metrics {
			switch mt.Name {
			case "State":
				st.State, _ = mt.Value.(string)
			case "Detail":
				st.Detail, _ = mt.Value.(string)
			}
		}
		n.Devices[t.Device] = st
	case sparkplug.NData:
		for _, s := range sig.FromMetrics(p.Metrics) {
			switch s.Name {
			case sig.Heartbeat:
				o.absorb(n, p.Metrics)
				n.LastSeen = time.Now()
				if n.Status != "online" {
					n.Status, n.Note = "online", ""
				}
			case sig.BandState:
				if t.Edge == model.BT {
					for i := 1; i <= 5; i++ {
						d := s.Str(fmt.Sprintf("D%d", i))
						if d == "" {
							d = s.Str(fmt.Sprintf("S%d", i))
						}
						if d != "" {
							o.sections[i].State = d
						}
					}
				}
			}
		}
	}
	o.mu.Unlock()
	o.kick()
}

// absorb stores the heartbeat metrics (devices and gauges) of a node.
func (o *observer) absorb(n *NodeState, ms []sparkplug.Metric) {
	pre := sig.Heartbeat + "/"
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

// watch marks nodes that stopped sending heartbeats (3 intervals, as the monitor does).
func (o *observer) watch(done <-chan struct{}) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-done:
			return
		case <-t.C:
		}
		changed := false
		o.mu.Lock()
		for _, n := range o.nodes {
			if n.Status == "online" && !n.LastSeen.IsZero() && time.Since(n.LastSeen) > 3*o.hb {
				n.Status, n.Note = "silent", "no heartbeat"
				changed = true
			}
		}
		o.mu.Unlock()
		if changed {
			o.kick()
		}
	}
}

// snapshot returns copies of the systems (a system split per station is merged from
// its stations), of the stations (every container, MON included from the host
// STATE message) and of the sections.
func (o *observer) snapshot() (systems, stations []NodeState, secs []Section, up bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	stations = o.stationList()
	by := map[string]NodeState{}
	for _, n := range stations {
		by[n.ID] = n
	}
	for _, sys := range model.Systems {
		ids := model.StationsOf(sys)
		if len(ids) == 1 {
			systems = append(systems, by[ids[0]])
			continue
		}
		systems = append(systems, mergeStations(sys, ids, by))
	}
	btUp := o.nodes[model.BT].Status == "online"
	for i := 1; i <= 5; i++ {
		s := o.sections[i]
		if !btUp {
			s.State = "off"
		}
		secs = append(secs, s)
	}
	return systems, stations, secs, o.hostUp || !o.hostSeen
}

// mergeStations makes one NodeState of a system from its stations: the worst status,
// the devices together, the gauges added up.
func mergeStations(sys string, ids []string, by map[string]NodeState) NodeState {
	m := NodeState{ID: sys, Status: "online", Devices: map[string]DevState{}, Gauges: map[string]int64{}, Stations: ids}
	rank := map[string]int{"online": 0, "unknown": 1, "silent": 2, "dead": 3}
	for i, id := range ids {
		n := by[id]
		if rank[n.Status] > rank[m.Status] {
			m.Status = n.Status
		}
		if n.Status != "online" {
			if m.Note != "" {
				m.Note += "; "
			}
			m.Note += id + " " + n.Status
		}
		m.Restarts += n.Restarts
		if i == 0 || n.Uptime < m.Uptime {
			m.Uptime = n.Uptime
		}
		if n.LastSeen.After(m.LastSeen) {
			m.LastSeen = n.LastSeen
		}
		for k, v := range n.Devices {
			m.Devices[k] = v
		}
		for k, v := range n.Gauges {
			m.Gauges[k] += v
		}
	}
	return m
}

func (o *observer) stationList() []NodeState {
	var out []NodeState
	for _, id := range model.Nodes {
		if id == model.MON {
			mon := NodeState{ID: model.MON, Devices: map[string]DevState{}, Gauges: map[string]int64{}}
			switch {
			case !o.hostSeen:
				mon.Status = "unknown"
			case o.hostUp:
				mon.Status = "online"
			default:
				mon.Status, mon.Note = "dead", "monitor offline (host STATE message)"
			}
			out = append(out, mon)
			continue
		}
		n := o.nodes[id]
		cp := *n
		cp.Devices = map[string]DevState{}
		for k, v := range n.Devices {
			cp.Devices[k] = v
		}
		cp.Gauges = map[string]int64{}
		for k, v := range n.Gauges {
			cp.Gauges[k] = v
		}
		out = append(out, cp)
	}
	return out
}
