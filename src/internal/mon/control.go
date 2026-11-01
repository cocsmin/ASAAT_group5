package mon

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"cakefactory/internal/scenario"
)

// component is one switchable part of the stack, as the control service
// reports it (docker compose: the ctl container; go run ./cmd/dev: the dev
// launcher).
type component struct {
	Name    string   `json:"name"`
	Label   string   `json:"label"`
	Kind    string   `json:"kind"`
	State   string   `json:"state"`
	Status  string   `json:"status"`
	Actions []string `json:"actions"`
	Locked  bool     `json:"locked,omitempty"`
}

var controlHC = &http.Client{Timeout: 45 * time.Second}

func controlURL() string { return strings.TrimRight(os.Getenv("CONTROL_URL"), "/") }

// canPause reports whether the control service can freeze a process (Docker can,
// the dev launcher cannot).
func canPause(cs []component) bool {
	for _, c := range cs {
		for _, a := range c.Actions {
			if a == "pause" || a == "unpause" {
				return true
			}
		}
	}
	return false
}

// runnable says whether a scenario can run from the dashboard right now.
func runnable(i scenario.Info, cs []component, avail bool) bool {
	if i.SelfStop || (i.Docker && !avail) || (i.NeedsPause && !canPause(cs)) {
		return false
	}
	have := map[string]bool{}
	for _, c := range cs {
		have[c.Name] = true
	}
	for _, n := range i.Needs {
		if !have[n] {
			return false
		}
	}
	return true
}

// components asks the control service for the state of the stack.
func components() ([]component, error) {
	u := controlURL()
	if u == "" {
		return nil, fmt.Errorf("CONTROL_URL is not set")
	}
	r, err := controlHC.Get(u + "/components")
	if err != nil {
		return nil, err
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		b, _ := io.ReadAll(r.Body)
		return nil, fmt.Errorf("%s: %s", r.Status, strings.TrimSpace(string(b)))
	}
	var cs []component
	if err := json.NewDecoder(r.Body).Decode(&cs); err != nil {
		return nil, err
	}
	return cs, nil
}

// control does one action on one component.
func control(service, action string) error {
	u := controlURL()
	if u == "" {
		return fmt.Errorf("CONTROL_URL is not set")
	}
	r, err := controlHC.Post(u+"/components/"+service+"/"+action, "text/plain", nil)
	if err != nil {
		return err
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		b, _ := io.ReadAll(r.Body)
		return fmt.Errorf("%s", strings.TrimSpace(string(b)))
	}
	return nil
}

type runState struct {
	Name    string    `json:"name"`
	Running bool      `json:"running"`
	Lines   []string  `json:"lines"`
	Result  string    `json:"result"` // "", pass, fail
	Started time.Time `json:"started"`
}

type scenarioRunner struct {
	mu  sync.Mutex
	run runState
}

func (m *Monitor) scenarioBase() string {
	addr := os.Getenv("HTTP_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	return "http://127.0.0.1:" + addr[strings.LastIndex(addr, ":")+1:]
}

func (m *Monitor) snapshotRun() runState {
	m.sr.mu.Lock()
	defer m.sr.mu.Unlock()
	cp := m.sr.run
	cp.Lines = append([]string(nil), m.sr.run.Lines...)
	return cp
}

func (m *Monitor) runLine(l string) {
	m.sr.mu.Lock()
	m.sr.run.Lines = append(m.sr.run.Lines, l)
	m.sr.mu.Unlock()
	m.broadcast("scenario", m.snapshotRun())
}

// runScenarios runs the named scenarios one after the other, in the background.
func (m *Monitor) runScenarios(names []string, avail bool) {
	failed := 0
	var ctl func(string, string) error
	if avail {
		ctl = control
	}
	infos := map[string]scenario.Info{}
	for _, i := range scenario.List() {
		infos[i.Name] = i
	}
	for _, n := range names {
		m.runLine("")
		m.runLine("== " + n + ": " + infos[n].Desc)
		start := time.Now()
		err := scenario.Run(n, scenario.Options{Base: m.scenarioBase(), Docker: avail, Control: ctl,
			Log: func(l string) { m.runLine("  " + l) }})
		if err != nil {
			failed++
			m.runLine("  FAIL " + n + ": " + err.Error())
			continue
		}
		m.runLine(fmt.Sprintf("  PASS %s (%.0fs)", n, time.Since(start).Seconds()))
	}
	m.runLine("")
	m.sr.mu.Lock()
	m.sr.run.Running = false
	m.sr.run.Result = "pass"
	if failed > 0 {
		m.sr.run.Result = "fail"
	}
	m.sr.mu.Unlock()
	m.runLine(fmt.Sprintf("%d of %d scenarios failed", failed, len(names)))
}

func (m *Monitor) registerControl(mux *http.ServeMux) {
	mux.HandleFunc("/api/components", func(w http.ResponseWriter, _ *http.Request) {
		out := map[string]any{"available": false, "items": []component{}}
		cs, err := components()
		if err != nil {
			out["hint"] = "Component control is not running. With Docker start the stack with: docker compose --profile demo up -d. With go run ./cmd/dev it is built in."
			out["error"] = err.Error()
			writeJSON(w, out)
			return
		}
		for i := range cs {
			if cs[i].Name == "mon" { // switching off the dashboard would cut this page
				cs[i].Locked, cs[i].Actions = true, nil
			}
		}
		out["available"], out["items"] = true, cs
		writeJSON(w, out)
	})
	mux.HandleFunc("/api/components/", func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/components/"), "/")
		if r.Method != http.MethodPost || len(parts) != 2 {
			http.Error(w, "POST /api/components/<name>/<action>", 400)
			return
		}
		if parts[0] == "mon" {
			http.Error(w, "the monitor cannot switch itself off from its own dashboard; use: go run ./cmd/scenario mon-down", 400)
			return
		}
		if err := control(parts[0], parts[1]); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		m.N.Log("UC2", fmt.Sprintf("%s: %s from the dashboard", parts[0], parts[1]), "")
		writeJSON(w, map[string]bool{"ok": true})
	})

	mux.HandleFunc("/api/scenarios", func(w http.ResponseWriter, _ *http.Request) {
		cs, err := components()
		avail := err == nil
		type item struct {
			scenario.Info
			Runnable bool `json:"runnable"`
		}
		var items []item
		for _, i := range scenario.List() {
			items = append(items, item{i, runnable(i, cs, avail)})
		}
		writeJSON(w, map[string]any{"available": avail, "items": items, "run": m.snapshotRun()})
	})
	mux.HandleFunc("/api/scenarios/run", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Name string `json:"name"`
		}
		if r.Method != http.MethodPost || json.NewDecoder(r.Body).Decode(&in) != nil {
			http.Error(w, "POST {name}", 400)
			return
		}
		cs, err := components()
		avail := err == nil
		var names []string
		for _, i := range scenario.List() {
			runnable := runnable(i, cs, avail)
			if in.Name == "all" && runnable || in.Name == i.Name {
				if !runnable {
					http.Error(w, i.Name+" cannot run from the dashboard: use go run ./cmd/scenario "+i.Name, 400)
					return
				}
				names = append(names, i.Name)
			}
		}
		if len(names) == 0 {
			http.Error(w, "unknown scenario "+in.Name, 404)
			return
		}
		m.sr.mu.Lock()
		if m.sr.run.Running {
			m.sr.mu.Unlock()
			http.Error(w, "a scenario is already running", 409)
			return
		}
		m.sr.run = runState{Name: in.Name, Running: true, Started: time.Now()}
		m.sr.mu.Unlock()
		m.broadcast("scenario", m.snapshotRun())
		go m.runScenarios(names, avail)
		writeJSON(w, map[string]bool{"ok": true})
	})
}
