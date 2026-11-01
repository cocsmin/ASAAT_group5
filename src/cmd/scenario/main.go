// Command scenario runs the demonstrations and checks of the cake factory
// against a running stack (docker compose up, or go run ./cmd/dev). Each
// scenario does something that goes wrong, prints a timeline of what the factory
// does about it, and ends with PASS or FAIL, so the same run can be shown to
// others and repeated by anyone. The dashboard can run the ones that do not
// need Docker too.
//
//	go run ./cmd/scenario list
//	go run ./cmd/scenario healthy crash-thm
//	go run ./cmd/scenario all
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"cakefactory/internal/scenario"
)

func main() {
	base := flag.String("url", "http://localhost:8080", "dashboard address of MON")
	dir := flag.String("compose-dir", ".", "directory with docker-compose.yml")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: scenario [flags] list | all | <name>...")
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
