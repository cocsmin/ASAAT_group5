// Package scenario holds the demonstrations and checks of the cake factory. A
// scenario does something that goes wrong in a running stack, reports a timeline
// of what the factory does about it, and ends in success or an error. It is used
// by the scenario command (terminal, CI) and by the dashboard.
package scenario

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

// Bounds from the THM requirements (THM-REQ-72, -73).
const (
	detectBound  = 15 * time.Second
	recoverBound = 60 * time.Second
)

type event struct {
	ID          int64  `json:"id"`
	Node        string `json:"node"`
	Severity    string `json:"severity"`
	Code        string `json:"code"`
	Reason      string `json:"reason"`
	Maintenance bool   `json:"maintenance"`
}

type ticket struct {
	ID        int64  `json:"id"`
	MachineID string `json:"machineId"`
	Status    string `json:"status"`
}

type nodeState struct {
	ID       string           `json:"id"`
	Status   string           `json:"status"`
	Restarts int              `json:"restarts"`
	Gauges   map[string]int64 `json:"gauges"`
}

type state struct {
	Sections []struct {
		State string `json:"state"`
	} `json:"sections"`
	Nodes   []nodeState `json:"nodes"`
	Tickets []ticket    `json:"tickets"`
	Alerts  []struct {
		Level string `json:"level"`
		Title string `json:"title"`
	} `json:"alerts"`
}

type env struct {
	metric     func(name string, v float64)
	faultAt    time.Time // when the fault of the running scenario was injected
	out        func(string)
	control    func(service, action string) error
	base       string
	composeDir string
	docker     bool
	t0         time.Time
	hc         *http.Client
}

func (e *env) logf(format string, a ...any) {
	e.out(fmt.Sprintf("%6.1fs  %s", time.Since(e.t0).Seconds(), fmt.Sprintf(format, a...)))
}

func (e *env) get(path string, v any) error {
	r, err := e.hc.Get(e.base + path)
	if err != nil {
		return err
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		return fmt.Errorf("%s: %s", path, r.Status)
	}
	return json.NewDecoder(r.Body).Decode(v)
}

func (e *env) post(path string, body any) error {
	b, _ := json.Marshal(body)
	r, err := e.hc.Post(e.base+path, "application/json", strings.NewReader(string(b)))
	if err != nil {
		return err
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		return fmt.Errorf("%s: %s", path, r.Status)
	}
	return nil
}

func (e *env) state() (state, error) {
	var s state
	return s, e.get("/api/state", &s)
}

func (e *env) events(since int64) ([]event, error) {
	var ev []event
	return ev, e.get(fmt.Sprintf("/api/events?since=%d", since), &ev)
}

func (e *env) lastEventID() int64 {
	ev, _ := e.events(0)
	var max int64
	for _, x := range ev {
		if x.ID > max {
			max = x.ID
		}
	}
	return max
}

func (e *env) pans() (int, map[string]int) {
	var s struct {
		Total    int            `json:"total"`
		ByStatus map[string]int `json:"byStatus"`
	}
	_ = e.get("/api/pans/summary", &s)
	return s.Total, s.ByStatus
}

func (e *env) gauge(node, name string) int64 {
	s, err := e.state()
	if err != nil {
		return 0
	}
	for _, n := range s.Nodes {
		if n.ID == node {
			return n.Gauges[name]
		}
	}
	return 0
}

// act does something to a container: through the control function when the
// caller has one (the dashboard), otherwise with the docker compose command.
func (e *env) act(action, service string) error {
	if e.control != nil {
		return e.control(service, action)
	}
	return e.compose(action, service)
}

func (e *env) compose(args ...string) error {
	cmd := exec.Command("docker", append([]string{"compose"}, args...)...)
	cmd.Dir = e.composeDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker compose %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// waitFor polls cond every 100 ms and returns how long it took.
func (e *env) waitFor(what string, timeout time.Duration, cond func() bool) (time.Duration, error) {
	start := time.Now()
	for time.Since(start) < timeout {
		if cond() {
			return time.Since(start), nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return 0, fmt.Errorf("timeout after %s waiting for: %s", timeout, what)
}

// waitEvent waits for an event after id that matches the filter.
func (e *env) waitEvent(since int64, timeout time.Duration, what string, match func(event) bool) (event, time.Duration, error) {
	var found event
	d, err := e.waitFor(what, timeout, func() bool {
		ev, err := e.events(since)
		if err != nil {
			return false
		}
		for _, x := range ev {
			if match(x) {
				found = x
				return true
			}
		}
		return false
	})
	return found, d, err
}

func (e *env) allOnline() bool {
	s, err := e.state()
	if err != nil || len(s.Nodes) == 0 {
		return false
	}
	for _, n := range s.Nodes {
		if n.Status != "online" {
			return false
		}
	}
	return true
}

func (e *env) noErrorAlerts() bool {
	s, err := e.state()
	if err != nil {
		return false
	}
	for _, a := range s.Alerts {
		if a.Level == "error" {
			return false
		}
	}
	return true
}

// settle waits until every node is online and no error alert is open.
func (e *env) settle() error {
	_, err := e.waitFor("all nodes online and no open error", 120*time.Second, func() bool { return e.allOnline() && e.noErrorAlerts() })
	return err
}

func (e *env) rec(name string, v float64) {
	if e.metric != nil {
		e.metric(name, v)
	}
}

func (e *env) flowResumes(since time.Duration) error {
	before := e.gauge("PLM", "cakes.shipped")
	d, err := e.waitFor("a pallet is shipped again", since, func() bool { return e.gauge("PLM", "cakes.shipped") > before })
	if err == nil {
		e.rec("flow_resume_s", d.Seconds())
		if !e.faultAt.IsZero() {
			e.rec("outage_s", time.Since(e.faultAt).Seconds()) // production outage: fault until the next cake ships
		}
		e.logf("flow resumed: cakes shipped %d -> %d (after %.0fs)", before, e.gauge("PLM", "cakes.shipped"), d.Seconds())
	}
	return err
}

// panLoop checks that no pan was lost or duplicated.
func (e *env) panLoop(want int) error {
	total, by := e.pans()
	e.rec("pans_lost", float64(want-total))
	e.logf("pans: %d in the loop %v", total, by)
	if total != want {
		return fmt.Errorf("pan count changed: %d, expected %d", total, want)
	}
	return nil
}

type scenario struct {
	name   string
	desc   string
	docker bool
	run    func(*env) error
}

func scenarios() []scenario {
	list := []scenario{
		{"healthy", "all five systems online and a pallet ships", false, healthy},
	}
	for _, n := range []string{"PLM", "BAT", "THM", "FIN"} {
		n := n
		list = append(list, scenario{"crash-" + strings.ToLower(n), "crash the " + n + " container (real process exit): detected, restarted, cakes and pans kept", false,
			func(e *env) error { return crash(e, n) }})
	}
	list = append(list,
		scenario{"freeze-thm", "freeze THM (no keep-alive): the broker declares it dead, MON halts, it returns", true, freeze},
		scenario{"mon-down", "stop MON while THM discards cakes: no event is lost, the line stops without the monitor", true, monDown},
		scenario{"sql-down", "switch SQL Server off for 30 s: the monitor must not mistake it for a dead line", true,
			func(e *env) error { return dbDown(e, "mssql", "SQL Server") }},
		scenario{"mongo-down", "switch MongoDB off for 30 s: stations carry on, documents catch up afterwards", true,
			func(e *env) error { return dbDown(e, "mongo", "MongoDB") }},
		scenario{"broker-restart", "restart the MQTT broker: every node reconnects, the line continues", true, brokerRestart},
		scenario{"cooling-chain", "cooling stops: the buffer fills (FC18), cakes wait too long (FC17), the repair releases them (FC16)", false, coolingChain},
		scenario{"faults-system", "failure cases that the system resolves itself (FC1 to FC26, UC4, UC5, UC12, UC14)", false,
			func(e *env) error { return faults(e, systemFaults) }},
		scenario{"faults-maintenance", "failure cases that need Maintenance: error, ticket, repair, recovery", false,
			func(e *env) error { return faults(e, maintenanceFaults) }},
	)
	return list
}

func healthy(e *env) error {
	if _, err := e.waitFor("all nodes online", 120*time.Second, e.allOnline); err != nil {
		return err
	}
	e.logf("all nodes online")
	return e.flowResumes(180 * time.Second)
}

// crash makes a controller exit like a real fault; Docker (or the dev launcher)
// restarts it. Checks THM-REQ-72 (detection) and THM-REQ-73 (recovery).
func crash(e *env, node string) error {
	if err := e.settle(); err != nil {
		return err
	}
	pans, _ := e.pans()
	since := e.lastEventID()
	t0 := time.Now()
	e.faultAt = time.Now()
	if err := e.post("/api/inject", map[string]any{"node": node, "code": "FC27", "count": 1}); err != nil {
		return err
	}
	e.logf("%s crashes now", node)
	_, d, err := e.waitEvent(since, detectBound, node+" death certificate", func(x event) bool {
		return x.Code == "FC27" && strings.HasPrefix(x.Reason, node+" container died")
	})
	if err != nil {
		return fmt.Errorf("not detected within %s: %w", detectBound, err)
	}
	e.rec("detect_s", d.Seconds())
	e.logf("MON saw the death certificate after %.1fs (bound %s)", d.Seconds(), detectBound)
	_, _, err = e.waitEvent(since, recoverBound, node+" back", func(x event) bool {
		return x.Code == "FC27" && strings.HasPrefix(x.Reason, node+" container is back")
	})
	if err != nil {
		return fmt.Errorf("not recovered within %s: %w", recoverBound, err)
	}
	e.rec("recover_s", time.Since(t0).Seconds())
	e.logf("%s is back after %.1fs (bound %s)", node, time.Since(t0).Seconds(), recoverBound)
	if ev, _ := e.events(since); len(ev) > 0 {
		for _, x := range ev {
			if x.Node == node && (strings.Contains(x.Reason, "taken up again") || strings.Contains(x.Reason, "put back in their queues")) {
				e.logf("%s: %s", x.Node, x.Reason)
			}
		}
	}
	if err := e.settle(); err != nil {
		return err
	}
	time.Sleep(5 * time.Second)
	if err := e.panLoop(pans); err != nil {
		return err
	}
	return e.flowResumes(180 * time.Second)
}

func freeze(e *env) error {
	if err := e.settle(); err != nil {
		return err
	}
	pans, _ := e.pans()
	since := e.lastEventID()
	t0 := time.Now()
	e.faultAt = time.Now()
	if err := e.act("pause", "thm"); err != nil {
		return err
	}
	e.logf("THM frozen (no keep-alive pings)")
	_, d, err := e.waitEvent(since, detectBound, "THM declared dead", func(x event) bool {
		return x.Code == "FC27" && strings.HasPrefix(x.Reason, "THM container died")
	})
	if err != nil {
		_ = e.act("unpause", "thm")
		return fmt.Errorf("not detected within %s: %w", detectBound, err)
	}
	e.rec("detect_s", d.Seconds())
	e.logf("broker declared THM dead after %.1fs, MON halted its band sections (bound %s)", d.Seconds(), detectBound)
	_ = time.Since(t0)
	if err := e.act("unpause", "thm"); err != nil {
		return err
	}
	e.logf("THM unfrozen")
	if _, _, err := e.waitEvent(since, recoverBound, "THM back", func(x event) bool {
		return x.Code == "FC27" && strings.HasPrefix(x.Reason, "THM container is back")
	}); err != nil {
		return err
	}
	if err := e.settle(); err != nil {
		return err
	}
	if err := e.panLoop(pans); err != nil {
		return err
	}
	return e.flowResumes(180 * time.Second)
}

// monDown stops MON while product that is already inside the tunnel is
// discarded. The events must still reach MON afterwards (THM-REQ-77).
func monDown(e *env) error {
	if err := e.settle(); err != nil {
		return err
	}
	before := e.lastEventID()
	if err := e.post("/api/inject", map[string]any{"node": "THM", "code": "FC12", "count": 4}); err != nil {
		return err
	}
	if err := e.act("stop", "mon"); err != nil {
		return err
	}
	e.logf("MON stopped; tunnel exit discards 4 cakes while nobody listens")
	time.Sleep(20 * time.Second)
	if err := e.act("start", "mon"); err != nil {
		return err
	}
	e.logf("MON started again")
	if _, err := e.waitFor("MON answers", 90*time.Second, func() bool { _, err := e.state(); return err == nil }); err != nil {
		return err
	}
	var got int
	_, err := e.waitFor("4 discard events reach MON", 60*time.Second, func() bool {
		ev, _ := e.events(before)
		got = 0
		for _, x := range ev {
			if x.Code == "FC12" && x.Severity == "discard" {
				got++
			}
		}
		return got >= 4
	})
	if err != nil {
		return fmt.Errorf("only %d of 4 discard events arrived: %w", got, err)
	}
	e.logf("all %d discard events arrived after MON came back (no event lost)", got)
	if err := e.settle(); err != nil {
		return err
	}
	return e.flowResumes(180 * time.Second)
}

func brokerRestart(e *env) error {
	if err := e.settle(); err != nil {
		return err
	}
	pans, _ := e.pans()
	if err := e.act("restart", "mosquitto"); err != nil {
		return err
	}
	e.logf("MQTT broker restarted (all connections dropped, retained messages gone)")
	time.Sleep(3 * time.Second)
	d, err := e.waitFor("all nodes online again", recoverBound, e.allOnline)
	if err != nil {
		return err
	}
	e.logf("all nodes online (%.1fs)", d.Seconds())
	if err := e.panLoop(pans); err != nil {
		return err
	}
	return e.flowResumes(180 * time.Second)
}

type fault struct {
	node, code string
	count      int
	expect     string // event code
	severity   string
	machine    string // ticket machine id prefix when Maintenance is needed
	what       string
}

var systemFaults = []fault{
	{"PLM", "FC1", 1, "FC1", "warning", "", "not enough ingredients: order held, Logistics notified, restock, order released"},
	{"PLM", "FC2", 1, "FC2", "warning", "", "warehouse not responding: circuit breaker, work continues with staged ingredients"},
	{"BAT", "FC3", 1, "FC3", "warning", "", "too little of an ingredient: missing amount requested, weighed again"},
	{"BAT", "FC4", 1, "FC4", "discard", "", "too much of an ingredient: batch discarded, new batch"},
	{"BAT", "UC4", 1, "UC4", "warning", "", "inspector rejects the batter: another mixing cycle"},
	{"THM", "FC7", 1, "FC7", "warning", "", "depositing stops: new batches held"},
	{"THM", "FC8", 1, "FC8", "warning", "", "no pan at depositing: batter held, pan requested"},
	{"THM", "UC5", 1, "UC5", "warning", "", "Weight-Checker out of calibration: depositing waits"},
	{"THM", "FC9", 1, "FC9", "discard", "", "wrong fill weight: pan diverted"},
	{"THM", "FC12", 1, "FC12", "discard", "", "cake under- or overcooked: diverted to refuse"},
	{"THM", "FC14", 1, "FC14", "warning", "", "cake still too hot after cooling: more cooling, buffer"},
	{"FIN", "FC21", 1, "FC21", "discard", "", "cake stuck or broken in the pan: rejected, pan washed"},
	{"FIN", "UC12", 1, "UC12", "discard", "", "washed pan damaged: removed from the cycle"},
	{"FIN", "FC24", 1, "FC24", "discard", "", "quality inspection rejects a cake (weight, visual or metal)"},
	{"FIN", "UC14", 1, "UC14", "discard", "", "wrap or carton defective: refuse bin"},
	{"FIN", "FC26", 1, "FC26", "warning", "", "wrong case weight: case set aside for a manual check"},
}

var maintenanceFaults = []fault{
	{"BAT", "FC5", 3, "FC5", "error", "bat-doser", "dosing fails 3 times in a row: doser locked out"},
	{"BAT", "FC6", 1, "FC6", "error", "bat-mixer", "mixer breaks: batch discarded"},
	{"THM", "FC10", 3, "FC10", "error", "thm-depositor", "wrong fill 3 times in a row: depositing stopped"},
	{"THM", "FC11", 1, "FC11", "error", "thm-zone-2", "baking zone temperature wrong: new pans stop, pans inside finish"},
	{"THM", "FC13", 1, "FC13", "error", "thm-heatcamera", "heat camera broken: all cakes diverted"},
	{"THM", "FC15", 1, "FC15", "error", "thm-cooling", "cooling stops: diverter raised, cakes go to the buffer"},
	{"FIN", "FC22", 1, "FC22", "error", "fin-depanner", "depanning machine breaks: cakes held"},
	{"FIN", "FC23", 1, "FC23", "error", "fin-washer", "washing station breaks: clean pans used from stock"},
	{"FIN", "FC25", 1, "FC25", "error", "fin-wrapper", "wrapping station breaks: holding area, pan admission stops"},
	{"MON", "FC19", 1, "FC19", "error", "mon-section-", "a band section stops: upstream halts, downstream runs empty"},
	{"MON", "FC20", 2, "FC20", "error", "mon-section-", "band speed wrong and the correction fails: Maintenance"},
	{"FIN", "FC24", 3, "FC24", "error", "fin-inspector", "the same quality check fails 3 times in a row: Maintenance"},
	{"THM", "FC28", 1, "FC28", "error", "thm-controller", "controller stops responding: MON marks it faulty"},
}

func faults(e *env, list []fault) error {
	if err := e.settle(); err != nil {
		return err
	}
	pans, _ := e.pans()
	var failed []string
	for _, f := range list {
		if err := runFault(e, f); err != nil {
			e.logf("FAIL %s %s: %v", f.node, f.code, err)
			failed = append(failed, f.code)
			_ = e.settle()
		}
	}
	if err := e.panLoop(pans); err != nil {
		failed = append(failed, "pan loop")
	}
	if len(failed) > 0 {
		return fmt.Errorf("failed: %s", strings.Join(failed, ", "))
	}
	return nil
}

func runFault(e *env, f fault) error {
	e.logf("%s %s: %s", f.node, f.code, f.what)
	since := e.lastEventID()
	t := time.Now()
	if err := e.post("/api/inject", map[string]any{"node": f.node, "code": f.code, "count": f.count}); err != nil {
		return err
	}
	ev, d, err := e.waitEvent(since, 150*time.Second, f.code+" "+f.severity+" event", func(x event) bool {
		return x.Code == f.expect && x.Severity == f.severity
	})
	if err != nil {
		return err
	}
	e.logf("   -> %s event after %.1fs: %s", ev.Severity, d.Seconds(), short(ev.Reason))
	if f.machine == "" {
		time.Sleep(2 * time.Second)
		return nil
	}
	var tk ticket
	if _, err := e.waitFor("a maintenance ticket for "+f.machine, 30*time.Second, func() bool {
		s, err := e.state()
		if err != nil {
			return false
		}
		for _, x := range s.Tickets {
			if x.Status == "open" && strings.HasPrefix(x.MachineID, f.machine) {
				tk = x
				return true
			}
		}
		return false
	}); err != nil {
		return err
	}
	e.logf("   -> Maintenance ticket %d opened for %s", tk.ID, tk.MachineID)
	time.Sleep(2 * time.Second)
	if err := e.post("/api/repair", map[string]any{"ticket": tk.ID}); err != nil {
		return err
	}
	e.logf("   -> repair confirmed")
	if err := e.settle(); err != nil {
		return err
	}
	e.logf("   -> recovered (%.0fs in total)", time.Since(t).Seconds())
	return nil
}

func short(s string) string {
	if len(s) > 90 {
		return s[:87] + "..."
	}
	return s
}

// ErrNeedsDocker is returned for a scenario that controls containers when the
// docker compose stack is not available to the caller.
var ErrNeedsDocker = errors.New("needs docker compose")

// Options say where the factory runs and where to send the timeline.
type Options struct {
	Base       string                             // dashboard address of MON
	ComposeDir string                             // directory with docker-compose.yml
	Docker     bool                               // containers can be controlled (docker compose or Control)
	Metric     func(name string, v float64)       // optional: receives measured values
	Control    func(service, action string) error // optional: acts on a container
	Log        func(string)                       // receives the timeline, one line at a time
}

// Info describes one scenario.
type Info struct {
	Name       string   `json:"name"`
	Desc       string   `json:"desc"`
	Docker     bool     `json:"docker"`     // controls containers
	SelfStop   bool     `json:"selfStop"`   // stops the dashboard itself: run it from a terminal
	NeedsPause bool     `json:"needsPause"` // freezes a process: needs Docker
	Needs      []string `json:"needs"`      // components that must exist (not in dev mode: mssql, mongo)
}

var needs = map[string][]string{"sql-down": {"mssql"}, "mongo-down": {"mongo"}}

// List returns the scenarios in the order they are run.
func List() []Info {
	var out []Info
	for _, s := range scenarios() {
		out = append(out, Info{Name: s.name, Desc: s.desc, Docker: s.docker, SelfStop: s.name == "mon-down", NeedsPause: s.name == "freeze-thm", Needs: needs[s.name]})
	}
	return out
}

// HasDocker reports whether docker compose answers for the stack in dir.
func HasDocker(dir string) bool {
	return exec.Command("docker", "compose", "-f", dir+"/docker-compose.yml", "ps", "-q").Run() == nil
}

// Run runs one scenario by name and returns nil when it passed.
func Run(name string, o Options) error {
	for _, s := range scenarios() {
		if s.name != name {
			continue
		}
		if o.Log == nil {
			o.Log = func(string) {}
		}
		if s.docker && !o.Docker {
			return ErrNeedsDocker
		}
		e := &env{out: o.Log, metric: o.Metric, control: o.Control, base: o.Base, composeDir: o.ComposeDir, docker: o.Docker,
			hc: &http.Client{Timeout: 5 * time.Second}, t0: time.Now()}
		if _, err := e.state(); err != nil {
			return fmt.Errorf("cannot reach %s: %w", o.Base, err)
		}
		return s.run(e)
	}
	return fmt.Errorf("unknown scenario %q", name)
}

// dbDown switches a database off and checks that the line is not halted and no
// node is declared dead because of it (the monitor's health path must not depend
// on the database), then switches it on and checks that the line carries on.
func dbDown(e *env, service, name string) error {
	if err := e.settle(); err != nil {
		return err
	}
	if err := e.act("stop", service); err != nil {
		return err
	}
	e.logf("%s switched off", name)
	var bad string
	_, _ = e.waitFor("the line to be halted (it must not be)", 30*time.Second, func() bool {
		st, err := e.state()
		if err != nil {
			bad = "the monitor does not answer"
			return true
		}
		for _, n := range st.Nodes {
			if n.Status != "online" {
				bad = n.ID + " is " + n.Status
				return true
			}
		}
		for i, sec := range st.Sections {
			if sec.State != "run" {
				bad = fmt.Sprintf("band section %d is %s", i+1, sec.State)
				return true
			}
		}
		return false
	})
	if err := e.act("start", service); err != nil {
		return err
	}
	if bad != "" {
		return fmt.Errorf("the line was halted while %s was off: %s", name, bad)
	}
	e.logf("30 s without %s: all nodes online, no band section halted", name)
	if err := e.settle(); err != nil {
		return err
	}
	return e.flowResumes(180 * time.Second)
}

// coolingChain follows FC15 through its consequences in the report: the cooling
// buffer fills (FC18), cakes that wait too long are discarded (FC17), and once
// cooling is repaired the buffered cakes are released (FC16).
func coolingChain(e *env) error {
	if err := e.settle(); err != nil {
		return err
	}
	since := e.lastEventID()
	if err := e.post("/api/inject", map[string]any{"node": "THM", "code": "FC15", "count": 1}); err != nil {
		return err
	}
	e.logf("cooling is about to stop (FC15)")
	steps := []struct {
		what     string
		code     string
		severity string
	}{
		{"cooling stops, diverter raised, cakes go to the buffer", "FC15", "error"},
		{"the buffer is full: new pans stop, cakes leaving the tunnel are diverted (FC18)", "FC18", "error"},
		{"a cake waited too long in the buffer and is discarded (FC17)", "FC17", "discard"},
	}
	for _, st := range steps {
		ev, d, err := e.waitEvent(since, 240*time.Second, st.what, func(x event) bool { return x.Code == st.code && x.Severity == st.severity })
		if err != nil {
			return err
		}
		e.logf("%s: %s (after %.0fs)", st.code, short(ev.Reason), d.Seconds())
	}
	var tk ticket
	if _, err := e.waitFor("a maintenance ticket for the cooling", 30*time.Second, func() bool {
		s, err := e.state()
		if err != nil {
			return false
		}
		for _, x := range s.Tickets {
			if x.Status == "open" && strings.HasPrefix(x.MachineID, "thm-cooling") {
				tk = x
				return true
			}
		}
		return false
	}); err != nil {
		return err
	}
	e.logf("Maintenance ticket %d opened for %s", tk.ID, tk.MachineID)
	repaired := e.lastEventID()
	if err := e.post("/api/repair", map[string]any{"ticket": tk.ID}); err != nil {
		return err
	}
	if ev, _, err := e.waitEvent(repaired, 60*time.Second, "FC16 buffered cakes released", func(x event) bool { return x.Code == "FC16" }); err != nil {
		return err
	} else {
		e.logf("FC16: %s", short(ev.Reason))
	}
	if err := e.settle(); err != nil {
		return err
	}
	return e.flowResumes(180 * time.Second)
}
