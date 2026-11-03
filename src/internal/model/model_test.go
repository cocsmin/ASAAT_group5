package model

import (
	"math/rand"
	"strings"
	"testing"
	"time"
)

func TestSystemOfAndStationsOf(t *testing.T) {
	cases := map[string]string{PLMWh: PLM, BATInsp: BAT, THMCool: THM, FINWash: FIN, BT: BT, MON: MON}
	for node, want := range cases {
		if got := SystemOf(node); got != want {
			t.Errorf("SystemOf(%q) = %q, want %q", node, got, want)
		}
	}
	if got := StationsOf(THM); len(got) != 4 || got[0] != THMDep || got[3] != THMCool {
		t.Errorf("StationsOf(THM) = %v", got)
	}
	if got := StationsOf(BT); len(got) != 1 || got[0] != BT {
		t.Errorf("a system with one container is its own station, got %v", got)
	}
}

func TestEverySystemHasAtLeastOneNode(t *testing.T) {
	for _, s := range Systems {
		if len(StationsOf(s)) == 0 {
			t.Errorf("system %s has no node", s)
		}
	}
	seen := map[string]bool{}
	for _, n := range Nodes {
		if seen[n] {
			t.Errorf("node %s listed twice", n)
		}
		seen[n] = true
	}
}

func TestNodeOfMachineUsesTheLongestStationPrefix(t *testing.T) {
	cases := map[string]string{
		"thm-bake-zone-2":     THMBake,
		"thm-bake-controller": THMBake,
		"thm-dep-depositor":   THMDep,
		"fin-wash-washer":     FINWash,
		"bat-mix-mixer":       BATMix,
		"bt-section-3":        BT,
		"plm-wh-controller":   PLMWh,
		"something-unknown":   "fallback",
		"thm-unknown-machine": "fallback",
	}
	for machine, want := range cases {
		if got := NodeOfMachine(machine, "fallback"); got != want {
			t.Errorf("NodeOfMachine(%q) = %q, want %q", machine, got, want)
		}
	}
}

func TestBandSectionsAreOwnedByStations(t *testing.T) {
	owners := map[int][]string{1: {THMDep}, 2: {THMBake}, 3: {THMExit, THMCool}, 4: {FINDpn, FINQua}, 5: {FINWrap, FINPal}}
	for sec, nodes := range owners {
		for _, n := range nodes {
			if !OwnsSection(n, sec) {
				t.Errorf("%s must own section %d", n, sec)
			}
		}
	}
	if OwnsSection(THMDep, 2) || OwnsSection(FINWash, 4) || OwnsSection(BATDose, 1) {
		t.Error("a station owns a section it does not work on")
	}
	for i := 1; i <= 5; i++ {
		if SectionName(i) == "" {
			t.Errorf("section %d has no name", i)
		}
	}
}

func TestFaultCatalogPointsAtRealNodes(t *testing.T) {
	nodes := map[string]bool{}
	for _, n := range Nodes {
		nodes[n] = true
	}
	for _, f := range FaultCatalog {
		if !nodes[f.Node] {
			t.Errorf("%s is injected into %q, which is not a node", f.Code, f.Node)
		}
		if f.Node == MON {
			t.Errorf("%s: the monitor has no failure cases of its own", f.Code)
		}
		if f.Resolved != "System" && f.Resolved != "Maintenance" {
			t.Errorf("%s has resolver %q", f.Code, f.Resolved)
		}
		if f.Count < 1 {
			t.Errorf("%s arms %d charges", f.Code, f.Count)
		}
	}
}

func TestEveryContainerCanCrashAndFreeze(t *testing.T) {
	has := map[string]map[string]bool{}
	for _, f := range FaultCatalog {
		if has[f.Node] == nil {
			has[f.Node] = map[string]bool{}
		}
		has[f.Node][f.Code] = true
	}
	for _, n := range Nodes {
		if n == MON {
			continue
		}
		if !has[n]["FC27"] || !has[n]["FC28"] {
			t.Errorf("%s needs FC27 (crash) and FC28 (stops responding) in the catalog", n)
		}
	}
}

func TestACooledCakeIsFarBelowTheLimitForPackaging(t *testing.T) {
	// a normal cake must never be refused by Finishing: the cooling mean is at least 4 scatters below the limit
	if TSafe-CoolMean < 4*CoolSigma {
		t.Fatalf("limit %.1f, cooling mean %.1f, scatter %.1f", TSafe, CoolMean, CoolSigma)
	}
}

func TestMachineIdsStartWithTheirStation(t *testing.T) {
	// the scenarios and the repair routing rely on this naming
	for _, m := range []string{"thm-dep-depositor", "thm-bake-zone-2", "thm-exit-heatcamera", "thm-cool-cooling",
		"fin-dpn-depanner", "fin-qua-inspector", "fin-wrap-wrapper", "fin-wash-washer", "bat-dose-doser", "bat-mix-mixer"} {
		node := NodeOfMachine(m, "")
		if node == "" || !strings.HasPrefix(m, strings.ToLower(node)+"-") {
			t.Errorf("machine %s does not map to a station (got %q)", m, node)
		}
	}
}

func TestChaosIsOffWithoutARate(t *testing.T) {
	always := func() float64 { return 0 }
	if got := ChaosDue(THMBake, 0, 1, time.Second, always); len(got) != 0 {
		t.Fatalf("FAULT_RATE 0 means no random failures, got %v", got)
	}
	if got := ChaosDue(THMBake, 1, 1, 0, always); len(got) != 0 {
		t.Fatal("no time, no failures")
	}
}

func TestChaosOnlyHitsTheNodeItIsAskedFor(t *testing.T) {
	for _, f := range ChaosDue(THMBake, 1, 1, time.Hour, func() float64 { return 0 }) {
		if f.Node != THMBake {
			t.Fatalf("%s belongs to %s", f.Code, f.Node)
		}
	}
	if len(ChaosDue(THMBake, 1, 1, time.Hour, func() float64 { return 0 })) == 0 {
		t.Fatal("with a roll of 0 every failure case of the node happens")
	}
	if got := ChaosDue("UNKNOWN", 1, 1, time.Hour, func() float64 { return 0 }); len(got) != 0 {
		t.Fatal("an unknown node has no failure cases")
	}
}

func TestChaosRateIsAPoissonRate(t *testing.T) {
	// a long run second by second: the number of failures of a node is close to the sum of its rates
	r := rand.New(rand.NewSource(7))
	hours := 1000.0
	n := 0
	for i := 0; i < int(hours*3600); i++ {
		n += len(ChaosDue(THMDep, 1, 1, time.Second, r.Float64))
	}
	want := 0.0
	for _, f := range FaultCatalog {
		if f.Node == THMDep {
			want += f.PerHour * hours
		}
	}
	if float64(n) < 0.95*want || float64(n) > 1.05*want {
		t.Fatalf("%d failures in %.0f hours, the rates say about %.0f", n, hours, want)
	}
}

func TestChaosScalesWithRateAndSimulationSpeed(t *testing.T) {
	count := func(mult, speed float64) int {
		r := rand.New(rand.NewSource(3))
		n := 0
		for i := 0; i < 400000; i++ {
			n += len(ChaosDue(FINWash, mult, speed, time.Second, r.Float64))
		}
		return n
	}
	base := count(1, 1)
	if base == 0 {
		t.Fatal("setup: no failure in the base run")
	}
	if x := count(4, 1); float64(x) < 3.4*float64(base) || float64(x) > 4.6*float64(base) {
		t.Errorf("4 times the rate: %d against %d", x, base)
	}
	if x := count(1, 4); float64(x) < 3.4*float64(base) || float64(x) > 4.6*float64(base) {
		t.Errorf("4 times the speed lives through 4 times the hours: %d against %d", x, base)
	}
}

func TestEveryFailureCaseHasARate(t *testing.T) {
	for _, f := range FaultCatalog {
		if f.PerHour <= 0 {
			t.Errorf("%s of %s has no rate", f.Code, f.Node)
		}
		if f.Resolved == "Maintenance" && f.PerHour > 0.5 {
			t.Errorf("%s needs Maintenance but happens %.2f times an hour: machines do not break that often", f.Code, f.PerHour)
		}
	}
}
