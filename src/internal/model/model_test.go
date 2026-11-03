package model

import (
	"strings"
	"testing"
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
