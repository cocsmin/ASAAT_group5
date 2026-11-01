// Command dev runs the whole factory on one machine without Docker: an
// embedded MQTT broker plus the five services, each restarted when it exits
// (the job Docker's restart policy does in the real deployment). Pan documents
// are stored as JSON files and SQL data is kept in memory.
//
//	go run ./cmd/dev
package main

import (
	"context"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
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
	names := []string{"devbroker", "mon", "plm", "bat", "thm", "fin"}
	for _, n := range names {
		out := filepath.Join(bin, n+ext)
		cmd := exec.CommandContext(ctx, "go", "build", "-o", out, "./cmd/"+n)
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			log.Fatalf("build %s: %v", n, err)
		}
	}
	env := append(os.Environ(), "MQTT_BROKER=tcp://127.0.0.1:1883", "PAN_DIR=data", "OUTBOX_DIR=data", "MQTT_KEEPALIVE_S=15")
	var wg sync.WaitGroup
	for i, n := range names {
		wg.Add(1)
		go func(n string) {
			defer wg.Done()
			for ctx.Err() == nil {
				cmd := exec.CommandContext(ctx, filepath.Join(bin, n+ext))
				cmd.Env, cmd.Stdout, cmd.Stderr = env, os.Stdout, os.Stderr
				err := cmd.Run()
				if ctx.Err() != nil {
					return
				}
				log.Printf("[dev] %s exited (%v): restarting like Docker would", n, err)
				time.Sleep(restartDelay)
			}
		}(n)
		if i == 0 {
			time.Sleep(time.Second) // let the broker listen first
		}
	}
	log.Println("[dev] factory running: open http://localhost:8080")
	wg.Wait()
}
