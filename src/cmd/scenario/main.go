// Command scenario runs the demonstrations and checks of the cake factory
// against a running stack (docker compose up, or go run ./cmd/dev). Each
// scenario does something that goes wrong, prints a timeline of what the factory
// does about it, and ends with PASS or FAIL, so the same run can be shown to
// others and repeated by anyone. The dashboard can run the ones that do not
// need Docker too.
//
//	go run ./cmd/scenario list
//	go run ./cmd/scenario healthy crash-thm-bake
//	go run ./cmd/scenario all
package main

import (
	"encoding/csv"
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"time"

	"cakefactory/internal/scenario"
)

func main() {
	base := flag.String("url", "http://localhost:8080", "dashboard address of MON")
	dir := flag.String("compose-dir", ".", "directory with docker-compose.yml")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: scenario [flags] list | all | <name>... | experiment [-runs N] [-out file.csv] [name...]")
		flag.PrintDefaults()
	}
	flag.Parse()
	args := flag.Args()
	all := scenario.List()
	if len(args) == 0 || args[0] == "list" {
		fmt.Println("scenarios (docker = needs the compose stack):")
		for _, s := range all {
			tag := "     "
			if s.Docker {
				tag = "docker"
			}
			fmt.Printf("  %-20s %s  %s\n", s.Name, tag, s.Desc)
		}
		return
	}
	var names []string
	if args[0] == "all" {
		for _, s := range all {
			names = append(names, s.Name)
		}
	} else {
		names = args
	}
	desc := map[string]string{}
	for _, s := range all {
		desc[s.Name] = s.Desc
	}
	docker := scenario.HasDocker(*dir)
	if args[0] == "experiment" {
		experiment(args[1:], *base, *dir, docker)
		return
	}
	failed := 0
	for _, n := range names {
		d, ok := desc[n]
		if !ok {
			fmt.Fprintf(os.Stderr, "unknown scenario %q; run: scenario list\n", n)
			os.Exit(2)
		}
		fmt.Printf("\n== %s: %s\n", n, d)
		start := time.Now()
		err := scenario.Run(n, scenario.Options{Base: *base, ComposeDir: *dir, Docker: docker,
			Log: func(l string) { fmt.Println("  " + l) }})
		switch {
		case errors.Is(err, scenario.ErrNeedsDocker):
			fmt.Println("  SKIP (needs docker compose)")
		case err != nil:
			fmt.Printf("  FAIL %s: %v\n", n, err)
			failed++
		default:
			fmt.Printf("  PASS %s (%.0fs)\n", n, time.Since(start).Seconds())
		}
	}
	fmt.Printf("\n%d of %d scenarios failed\n", failed, len(names))
	if failed > 0 {
		os.Exit(1)
	}
}

// experiment repeats the availability scenarios and writes one CSV row per run
// (see docs/experiment.md). Default: every crash scenario and the frozen node.
func experiment(args []string, base, dir string, docker bool) {
	fs := flag.NewFlagSet("experiment", flag.ExitOnError)
	runs := fs.Int("runs", 10, "runs per scenario")
	out := fs.String("out", "experiment-results.csv", "CSV file to write")
	_ = fs.Parse(args)
	names := fs.Args()
	if len(names) == 0 {
		names = []string{"crash-plm-wh", "crash-plm-ship", "crash-bat-dose", "crash-bat-mix", "crash-bat-insp", "crash-thm-dep", "crash-thm-bake", "crash-thm-exit", "crash-thm-cool", "crash-fin-dpn", "crash-fin-qua", "crash-fin-wrap", "crash-fin-pal", "crash-fin-wash"}
		if docker {
			names = append(names, "freeze-thm-bake")
		}
	}
	file, err := os.Create(*out)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer file.Close()
	w := csv.NewWriter(file)
	cols := []string{"detect_s", "recover_s", "outage_s", "flow_resume_s", "pans_lost"}
	_ = w.Write(append([]string{"timestamp", "scenario", "run", "result"}, append(cols, "error")...))
	failed := 0
	for _, n := range names {
		for i := 1; i <= *runs; i++ {
			vals := map[string]float64{}
			err := scenario.Run(n, scenario.Options{Base: base, ComposeDir: dir, Docker: docker,
				Metric: func(k string, v float64) { vals[k] = v }, Log: func(string) {}})
			row := []string{time.Now().UTC().Format(time.RFC3339), n, strconv.Itoa(i), "pass"}
			msg := ""
			if err != nil {
				row[3], msg = "fail", err.Error()
				failed++
			}
			for _, c := range cols {
				if v, ok := vals[c]; ok {
					row = append(row, strconv.FormatFloat(v, 'f', 2, 64))
				} else {
					row = append(row, "")
				}
			}
			_ = w.Write(append(row, msg))
			w.Flush()
			fmt.Printf("%-12s run %2d/%d  %s  %v\n", n, i, *runs, row[3], vals)
		}
	}
	fmt.Printf("\nwrote %s (%d failed runs)\n", *out, failed)
	if failed > 0 {
		os.Exit(1)
	}
}
