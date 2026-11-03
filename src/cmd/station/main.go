// Command station runs one station of a system that is split per station. The
// station is chosen by the STATION environment variable (or the first argument),
// for example STATION=thm-bake.
package main

import (
	"log"
	"os"
	"strings"

	"cakefactory/internal/thm"
)

var stations = map[string]func(){
	"thm-dep":  thm.RunDep,
	"thm-bake": thm.RunBake,
	"thm-exit": thm.RunExit,
	"thm-cool": thm.RunCool,
}

func main() {
	name := os.Getenv("STATION")
	if len(os.Args) > 1 {
		name = os.Args[1]
	}
	run, ok := stations[strings.ToLower(name)]
	if !ok {
		log.Fatalf("unknown station %q (set STATION)", name)
	}
	run()
}
