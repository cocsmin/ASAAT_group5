package scenario

import (
	"strings"
	"testing"

	"cakefactory/internal/model"
)

func TestScenarioNamesAreUniqueAndDescribed(t *testing.T) {
	seen := map[string]bool{}
	for _, i := range List() {
		if seen[i.Name] {
			t.Errorf("scenario %s is listed twice", i.Name)
		}
		seen[i.Name] = true
		if len(i.Desc) < 20 {
			t.Errorf("scenario %s has no description", i.Name)
		}
		if strings.ToLower(i.Name) != i.Name || strings.Contains(i.Name, " ") {
			t.Errorf("scenario name %q must be lower case without spaces", i.Name)
		}
	}
}

func TestEveryContainerHasACrashScenario(t *testing.T) {
	have := map[string]bool{}
	for _, i := range List() {
		have[i.Name] = true
	}
	for _, n := range model.Nodes {
		if n == model.MON {
			continue // mon-down is its own scenario
		}
		if !have["crash-"+strings.ToLower(n)] {
			t.Errorf("no crash scenario for %s", n)
		}
	}
	if !have["mon-down"] {
		t.Error("no scenario for a monitor that is down")
	}
}

func TestSeveralStationsFailingTogetherIsCovered(t *testing.T) {
	found := 0
	for _, i := range List() {
		if strings.HasPrefix(i.Name, "freeze-t") && strings.Contains(i.Name, "stations") {
			found++
			if !i.NeedsPause || !i.Docker {
				t.Errorf("%s freezes processes: it needs Docker", i.Name)
			}
		}
	}
	if found < 2 {
		t.Fatalf("want a scenario with two and one with three stations failing at once, found %d", found)
	}
}

func TestFaultTablesPointAtNodesAndMachinesOfThatNode(t *testing.T) {
	for _, list := range [][]fault{systemFaults, maintenanceFaults} {
		for _, f := range list {
			known := false
			for _, n := range model.Nodes {
				known = known || n == f.node
			}
			if !known {
				t.Errorf("%s is injected into %q, which is no node", f.code, f.node)
			}
			if f.machine != "" && !strings.HasPrefix(f.machine, "bt-section-") && model.NodeOfMachine(f.machine, "") != f.node {
				t.Errorf("%s: machine %s belongs to %q, not to %s", f.code, f.machine, model.NodeOfMachine(f.machine, ""), f.node)
			}
		}
	}
}

func TestEveryInjectedCodeIsInTheCatalog(t *testing.T) {
	catalog := map[string]bool{}
	for _, c := range model.FaultCatalog {
		catalog[c.Node+"/"+c.Code] = true
	}
	for _, list := range [][]fault{systemFaults, maintenanceFaults} {
		for _, f := range list {
			if !catalog[f.node+"/"+f.code] {
				t.Errorf("%s into %s is not in the fault catalog, the dashboard has no button for it", f.code, f.node)
			}
		}
	}
}
