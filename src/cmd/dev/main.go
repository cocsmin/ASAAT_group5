// Command dev runs the whole factory on one machine without Docker: an
// embedded MQTT broker plus the five services, each restarted when it crashes
// (the job Docker's restart policy does in the real deployment). Pan documents
// are stored as JSON files and SQL data is kept in memory. It also serves the
// component control API (stop, start, restart) that the dashboard uses, the
// same one the ctl container serves in the Docker deployment.
//
//	go run ./cmd/dev
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// restartDelay is how long a crashed service stays down; raise it with
// DEV_RESTART_SECONDS to test outages.
var restartDelay = func() time.Duration {
	if v, err := strconv.ParseFloat(os.Getenv("DEV_RESTART_SECONDS"), 64); err == nil && v > 0 {
		return time.Duration(v * float64(time.Second))
	}
	return 2 * time.Second
}()

// service is one process of the factory.
type service struct {
	name    string // component name, as in docker-compose.yml
	label   string
	kind    string
	binary  string
	mu      sync.Mutex
	wanted  bool // false after a deliberate stop
	running bool
	cancel  context.CancelFunc
	restart bool // deliberate restart: no delay
}

func (s *service) state() (string, []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case !s.wanted:
		return "exited", []string{"start"}
	case s.running:
		return "running", []string{"stop", "restart"}
	default:
		return "restarting", []string{"stop"}
	}
}

func (s *service) do(action string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch action {
	case "stop":
		s.wanted = false
		if s.cancel != nil {
			s.cancel()
		}
	case "start":
		s.wanted = true
	case "restart":
		s.restart = true
		s.wanted = true
		if s.cancel != nil {
			s.cancel()
		}
	default:
		return fmt.Errorf("%s is not available without Docker", action)
	}
	return nil
}

func (s *service) loop(ctx context.Context, bin string, env []string) {
	for ctx.Err() == nil {
		s.mu.Lock()
		wanted := s.wanted
		s.mu.Unlock()
		if !wanted {
			time.Sleep(300 * time.Millisecond)
			continue
		}
		cctx, cancel := context.WithCancel(ctx)
		cmd := exec.CommandContext(cctx, filepath.Join(bin, s.binary))
		cmd.Env, cmd.Stdout, cmd.Stderr = env, os.Stdout, os.Stderr
		s.mu.Lock()
		s.cancel, s.running = cancel, true
		s.mu.Unlock()
		err := cmd.Run()
		cancel()
		s.mu.Lock()
		s.running, s.cancel = false, nil
		stillWanted, immediate := s.wanted, s.restart
		s.restart = false
		s.mu.Unlock()
		if ctx.Err() != nil {
			return
		}
		if !stillWanted {
			log.Printf("[dev] %s stopped", s.name)
			continue
		}
		if immediate {
			log.Printf("[dev] %s restarting", s.name)
			continue
		}
		log.Printf("[dev] %s exited (%v): restarting like Docker would", s.name, err)
		time.Sleep(restartDelay)
	}
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if os.Getenv("DEV_KEEP_DATA") == "" { // SQL data is in memory in dev mode, so start the documents clean too
		_ = os.RemoveAll("data")
	}
	bin, err := os.MkdirTemp("", "cakefactory-bin")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(bin)
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	services := []*service{
		{name: "mosquitto", label: "MQTT broker (embedded)", kind: "infra", binary: "devbroker" + ext},
		{name: "mon", label: "Line Monitoring & Health", kind: "app", binary: "mon" + ext},
		{name: "plm", label: "Production & Logistics", kind: "app", binary: "plm" + ext},
		{name: "bat", label: "Batter Preparation", kind: "app", binary: "bat" + ext},
		{name: "thm", label: "Thermal Processing", kind: "app", binary: "thm" + ext},
		{name: "fin", label: "Finishing & Packaging", kind: "app", binary: "fin" + ext},
	}
	for _, s := range services {
		s.wanted = true
		pkg := strings.TrimSuffix(s.binary, ext)
		cmd := exec.CommandContext(ctx, "go", "build", "-o", filepath.Join(bin, s.binary), "./cmd/"+pkg)
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			log.Fatalf("build %s: %v", pkg, err)
		}
	}
	env := append(os.Environ(), "MQTT_BROKER=tcp://127.0.0.1:1883", "PAN_DIR=data", "OUTBOX_DIR=data",
		"MQTT_KEEPALIVE_S=15", "CONTROL_URL=http://127.0.0.1:8090")
	go serveControl(ctx, services)
	var wg sync.WaitGroup
	for i, s := range services {
		wg.Add(1)
		go func(s *service) {
			defer wg.Done()
			s.loop(ctx, bin, env)
		}(s)
		if i == 0 {
			time.Sleep(time.Second) // let the broker listen first
		}
	}
	log.Println("[dev] factory running: open http://localhost:8080")
	wg.Wait()
}

// serveControl is the component control API for the dashboard (same shape as
// cmd/ctl): GET /components, POST /components/<name>/<action>.
func serveControl(ctx context.Context, services []*service) {
	mux := http.NewServeMux()
	mux.HandleFunc("/components", func(w http.ResponseWriter, _ *http.Request) {
		type comp struct {
			Name    string   `json:"name"`
			Label   string   `json:"label"`
			Kind    string   `json:"kind"`
			State   string   `json:"state"`
			Status  string   `json:"status"`
			Actions []string `json:"actions"`
		}
		out := []comp{}
		for _, s := range services {
			st, acts := s.state()
			out = append(out, comp{s.name, s.label, s.kind, st, st, acts})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	})
	mux.HandleFunc("/components/", func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/components/"), "/")
		if r.Method != http.MethodPost || len(parts) != 2 {
			http.Error(w, "POST /components/<name>/<action>", 400)
			return
		}
		for _, s := range services {
			if s.name == parts[0] {
				if err := s.do(parts[1]); err != nil {
					http.Error(w, err.Error(), 409)
					return
				}
				log.Printf("[dev] %s %s", parts[1], s.name)
				fmt.Fprint(w, "ok")
				return
			}
		}
		http.Error(w, "unknown component "+parts[0], 404)
	})
	srv := &http.Server{Addr: "127.0.0.1:8090", Handler: mux}
	go func() {
		<-ctx.Done()
		srv.Close()
	}()
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Printf("[dev] control API: %v", err)
	}
}
