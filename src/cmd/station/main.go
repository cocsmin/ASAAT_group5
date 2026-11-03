// Command station runs one station of a system that is split per station. The
// station is chosen by the STATION environment variable (or the first argument),
// for example STATION=thm-bake.
package main

import (
	"log"
	"os"
	"strings"

	"cakefactory/internal/bat"
	"cakefactory/internal/fin"
	"cakefactory/internal/plm"
	"cakefactory/internal/thm"
)

var stations = map[string]func(){
	"plm-wh":   plm.RunWh,
	"plm-ship": plm.RunShip,
	"bat-dose": bat.RunDose,
	"bat-mix":  bat.RunMix,
	"bat-insp": bat.RunInsp,
	"thm-dep":  thm.RunDep,
	"thm-bake": thm.RunBake,
	"thm-exit": thm.RunExit,
	"thm-cool": thm.RunCool,
	"fin-dpn":  fin.RunDpn,
	"fin-qua":  fin.RunQua,
	"fin-wrap": fin.RunWrap,
	"fin-pal":  fin.RunPal,
	"fin-wash": fin.RunWash,
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
