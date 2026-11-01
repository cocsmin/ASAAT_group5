// Command scenario runs the demonstrations and checks of the cake factory
// against a running stack (docker compose up, or go run ./cmd/dev). Each
// scenario does something that goes wrong, prints a timeline of what the factory
// does about it, and ends with PASS or FAIL, so the same run can be shown to
// others and repeated by anyone.
//
//	go run ./cmd/scenario list
//	go run ./cmd/scenario healthy crash-thm
//	go run ./cmd/scenario all
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"sort"
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
	Nodes   []nodeState `json:"nodes"`
	Tickets []ticket    `json:"tickets"`
	Alerts  []struct {
		Level string `json:"level"`
		Title string `json:"title"`
	} `json:"alerts"`
}

type env struct {
	base       string
	composeDir string
	docker     bool
	t0         time.Time
	hc         *http.Client
}

func (e *env) logf(format string, a ...any) {
	fmt.Printf("  %6.1fs  %s\n", time.Since(e.t0).Seconds(), fmt.Sprintf(format, a...))
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

func (e *env) flowResumes(since time.Duration) error {
	before := e.gauge("PLM", "cakes.shipped")
	d, err := e.waitFor("a pallet is shipped again", since, func() bool { return e.gauge("PLM", "cakes.shipped") > before })
	if err == nil {
		e.logf("flow resumed: cakes shipped %d -> %d (after %.0fs)", before, e.gauge("PLM", "cakes.shipped"), d.Seconds())
	}
	return err
}

// panLoop checks that no pan was lost or duplicated.
func (e *env) panLoop(want int) error {
	total, by := e.pans()
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
		scenario{"broker-restart", "restart the MQTT broker: every node reconnects, the line continues", true, brokerRestart},
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
	e.logf("MON saw the death certificate after %.1fs (bound %s)", d.Seconds(), detectBound)
	_, _, err = e.waitEvent(since, recoverBound, node+" back", func(x event) bool {
		return x.Code == "FC27" && strings.HasPrefix(x.Reason, node+" container is back")
	})
	if err != nil {
		return fmt.Errorf("not recovered within %s: %w", recoverBound, err)
	}
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
	if err := e.compose("pause", "thm"); err != nil {
		return err
	}
	e.logf("THM frozen (no keep-alive pings)")
	_, d, err := e.waitEvent(since, detectBound, "THM declared dead", func(x event) bool {
		return x.Code == "FC27" && strings.HasPrefix(x.Reason, "THM container died")
	})
	if err != nil {
		_ = e.compose("unpause", "thm")
		return fmt.Errorf("not detected within %s: %w", detectBound, err)
	}
	e.logf("broker declared THM dead after %.1fs, MON halted its band sections (bound %s)", d.Seconds(), detectBound)
	_ = time.Since(t0)
	if err := e.compose("unpause", "thm"); err != nil {
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
	if err := e.compose("stop", "mon"); err != nil {
		return err
	}
	e.logf("MON stopped; tunnel exit discards 4 cakes while nobody listens")
	time.Sleep(20 * time.Second)
	if err := e.compose("start", "mon"); err != nil {
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
	if err := e.compose("restart", "mosquitto"); err != nil {
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

func main() {
	base := flag.String("url", "http://localhost:8080", "dashboard address of MON")
	dir := flag.String("compose-dir", ".", "directory with docker-compose.yml")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: scenario [flags] list | all | <name>...")
		flag.PrintDefaults()
	}
	flag.Parse()
	all := scenarios()
	args := flag.Args()
	if len(args) == 0 || args[0] == "list" {
		fmt.Println("scenarios (docker = needs the compose stack):")
		for _, s := range all {
			tag := "     "
			if s.docker {
				tag = "docker"
			}
			fmt.Printf("  %-20s %s  %s\n", s.name, tag, s.desc)
		}
		return
	}
	e := &env{base: *base, composeDir: *dir, hc: &http.Client{Timeout: 5 * time.Second}}
	e.docker = exec.Command("docker", "compose", "-f", *dir+"/docker-compose.yml", "ps", "-q").Run() == nil
	var run []scenario
	if args[0] == "all" {
		run = all
	} else {
		byName := map[string]scenario{}
		for _, s := range all {
			byName[s.name] = s
		}
		for _, a := range args {
			s, ok := byName[a]
			if !ok {
				names := make([]string, 0, len(byName))
				for n := range byName {
					names = append(names, n)
				}
				sort.Strings(names)
				fmt.Fprintf(os.Stderr, "unknown scenario %q; known: %s\n", a, strings.Join(names, ", "))
				os.Exit(2)
			}
			run = append(run, s)
		}
	}
	failed := 0
	for _, s := range run {
		fmt.Printf("\n== %s: %s\n", s.name, s.desc)
		if s.docker && !e.docker {
			fmt.Println("  SKIP (needs docker compose)")
			continue
		}
		e.t0 = time.Now()
		if _, err := e.state(); err != nil {
			fmt.Printf("  FAIL cannot reach %s: %v\n", *base, err)
			failed++
			continue
		}
		if err := s.run(e); err != nil {
			fmt.Printf("  FAIL %s: %v\n", s.name, err)
			failed++
			continue
		}
		fmt.Printf("  PASS %s (%.0fs)\n", s.name, time.Since(e.t0).Seconds())
	}
	fmt.Printf("\n%d of %d scenarios failed\n", failed, len(run))
	if failed > 0 {
		os.Exit(1)
	}
}
