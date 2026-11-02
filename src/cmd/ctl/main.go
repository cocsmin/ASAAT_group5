// Command ctl is the component control service of the demo: it lets the
// dashboard stop, start, pause and restart the containers of the stack, so the
// effect of switching a part off can be seen live. It talks to the Docker Engine
// API through the Docker socket, which gives it control over the host's
// containers, so it only runs under the "demo" profile of the compose file
// (docker compose --profile demo up -d). The dashboard is the only caller.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"
)

// Order and kind of the components in the dashboard.
var order = []string{"mosquitto", "mssql", "mongo", "plm", "bat", "thm", "fin", "bt", "mon", "ui"}

type component struct {
	Name    string   `json:"name"`
	Label   string   `json:"label"`
	Kind    string   `json:"kind"`
	State   string   `json:"state"`
	Status  string   `json:"status"`
	Actions []string `json:"actions"`
	id      string
}

var labels = map[string]string{
	"mosquitto": "MQTT broker (Sparkplug B)", "mssql": "SQL Server", "mongo": "MongoDB",
	"plm": "Production & Logistics", "bat": "Batter Preparation", "thm": "Thermal Processing",
	"fin": "Finishing & Packaging", "bt": "Band Transport", "mon": "Line Monitoring & Health", "ui": "Experiment dashboard",
}

type docker struct {
	hc      *http.Client
	project string
}

func newDocker() *docker {
	sock := os.Getenv("DOCKER_SOCK")
	if sock == "" {
		sock = "/var/run/docker.sock"
	}
	return &docker{hc: &http.Client{
		Timeout: 40 * time.Second,
		Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", sock)
		}},
	}}
}

func (d *docker) call(method, path string, out any) error {
	req, err := http.NewRequest(method, "http://docker"+path, nil)
	if err != nil {
		return err
	}
	r, err := d.hc.Do(req)
	if err != nil {
		return err
	}
	defer r.Body.Close()
	b, _ := io.ReadAll(r.Body)
	if r.StatusCode >= 300 && r.StatusCode != 304 {
		return fmt.Errorf("docker %s %s: %s %s", method, path, r.Status, strings.TrimSpace(string(b)))
	}
	if out != nil && len(b) > 0 {
		return json.Unmarshal(b, out)
	}
	return nil
}

// discover finds the compose project this container belongs to.
func (d *docker) discover() error {
	if p := os.Getenv("COMPOSE_PROJECT"); p != "" {
		d.project = p
		return nil
	}
	var self struct {
		Config struct{ Labels map[string]string }
	}
	if err := d.call("GET", "/containers/"+os.Getenv("HOSTNAME")+"/json", &self); err != nil {
		return err
	}
	d.project = self.Config.Labels["com.docker.compose.project"]
	if d.project == "" {
		return fmt.Errorf("this container has no compose project label")
	}
	return nil
}

func (d *docker) list() ([]component, error) {
	filters, _ := json.Marshal(map[string][]string{"label": {"com.docker.compose.project=" + d.project}})
	var cs []struct {
		ID     string
		Labels map[string]string
		State  string
		Status string
	}
	if err := d.call("GET", "/containers/json?all=1&filters="+url.QueryEscape(string(filters)), &cs); err != nil {
		return nil, err
	}
	var out []component
	for _, c := range cs {
		name := c.Labels["com.docker.compose.service"]
		if name == "" || name == "ctl" {
			continue
		}
		kind := "app"
		if name == "mosquitto" || name == "mssql" || name == "mongo" {
			kind = "infra"
		}
		label := labels[name]
		if label == "" {
			label = name
		}
		out = append(out, component{Name: name, Label: label, Kind: kind, State: c.State, Status: c.Status, Actions: actions(c.State), id: c.ID})
	}
	sort.Slice(out, func(i, j int) bool { return rank(out[i].Name) < rank(out[j].Name) })
	return out, nil
}

func rank(n string) int {
	for i, o := range order {
		if o == n {
			return i
		}
	}
	return len(order)
}

func actions(state string) []string {
	switch state {
	case "running":
		return []string{"stop", "pause", "restart"}
	case "paused":
		return []string{"unpause", "stop"}
	case "restarting":
		return []string{"stop"}
	default:
		return []string{"start"}
	}
}

func main() {
	d := newDocker()
	for {
		if err := d.discover(); err != nil {
			log.Printf("ctl: waiting for the Docker socket: %v", err)
			time.Sleep(3 * time.Second)
			continue
		}
		break
	}
	log.Printf("ctl: controlling compose project %q", d.project)
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, "ok") })
	mux.HandleFunc("/components", func(w http.ResponseWriter, _ *http.Request) {
		cs, err := d.list()
		if err != nil {
			http.Error(w, err.Error(), 502)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(cs)
	})
	mux.HandleFunc("/components/", func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/components/"), "/")
		if r.Method != http.MethodPost || len(parts) != 2 {
			http.Error(w, "POST /components/<name>/<action>", 400)
			return
		}
		name, action := parts[0], parts[1]
		cs, err := d.list()
		if err != nil {
			http.Error(w, err.Error(), 502)
			return
		}
		for _, c := range cs {
			if c.Name != name {
				continue
			}
			ok := false
			for _, a := range c.Actions {
				ok = ok || a == action
			}
			if !ok {
				http.Error(w, fmt.Sprintf("%s is %s: cannot %s", name, c.State, action), 409)
				return
			}
			path := "/containers/" + c.id + "/" + action
			if action == "stop" || action == "restart" {
				path += "?t=2"
			}
			if err := d.call("POST", path, nil); err != nil {
				http.Error(w, err.Error(), 502)
				return
			}
			log.Printf("ctl: %s %s", action, name)
			fmt.Fprint(w, "ok")
			return
		}
		http.Error(w, "unknown component "+name, 404)
	})
	addr := os.Getenv("CTL_ADDR")
	if addr == "" {
		addr = ":8090"
	}
	log.Fatal(http.ListenAndServe(addr, mux))
}
