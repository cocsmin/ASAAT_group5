package bt

import "testing"

func newBT() *BT {
	t := &BT{target: nominalSpeed}
	for i := 1; i <= 5; i++ {
		t.cmd[i] = "run"
	}
	return t
}

func states(t *BT) [6]int {
	var s [6]int
	for i := 1; i <= 5; i++ {
		s[i] = t.effective(i)
	}
	return s
}

func TestAllSectionsRunByDefault(t *testing.T) {
	s := states(newBT())
	for i := 1; i <= 5; i++ {
		if s[i] != stRun {
			t.Fatalf("section %d: %v", i, s)
		}
	}
}

func TestHaltedSectionStopsEverythingUpstreamOnly(t *testing.T) {
	b := newBT()
	b.cmd[3] = "halt" // the monitor halts the cooling stations
	s := states(b)
	want := [6]int{0, stHalt, stHalt, stHalt, stRun, stRun}
	if s != want {
		t.Fatalf("a halted section 3 halts 1 and 2 and leaves 4 and 5 running: got %v", s)
	}
}

func TestHaltOfTheLastSectionHaltsTheWholeBand(t *testing.T) {
	b := newBT()
	b.cmd[5] = "halt"
	for i := 1; i <= 5; i++ {
		if b.effective(i) != stHalt {
			t.Fatalf("section %d must be halted behind a halted section 5", i)
		}
	}
}

func TestHaltOfTheFirstSectionHaltsOnlyThat(t *testing.T) {
	b := newBT()
	b.cmd[1] = "halt"
	s := states(b)
	if s[1] != stHalt || s[2] != stRun {
		t.Fatalf("nothing is upstream of section 1: %v", s)
	}
}

func TestStoppedSectionIsFaultAndWinsOverHalt(t *testing.T) {
	b := newBT()
	b.fault[2] = true
	b.hold[1] = true
	b.cmd[2] = "halt"
	s := states(b)
	if s[2] != stFault || s[1] != stHalt || s[3] != stRun {
		t.Fatalf("fault on 2, upstream halted, downstream running: %v", s)
	}
}

func TestSlowDownOnlyWhereCommandedOrWrongSpeed(t *testing.T) {
	b := newBT()
	b.cmd[2] = "slow"
	b.slow[4] = true
	s := states(b)
	if s[2] != stSlow || s[4] != stSlow || s[1] != stRun || s[3] != stRun {
		t.Fatalf("slow on 2 and 4 only: %v", s)
	}
}

func TestHaltBeatsSlowDown(t *testing.T) {
	b := newBT()
	b.cmd[2] = "slow"
	b.hold[2] = true
	if b.effective(2) != stHalt {
		t.Fatal("a halted section does not run slowly")
	}
}

func TestRestartOfTheDownstreamSectionReleasesTheUpstreamOnes(t *testing.T) {
	b := newBT()
	b.cmd[4] = "halt"
	if b.effective(1) != stHalt {
		t.Fatal("setup: section 1 is halted behind section 4")
	}
	b.cmd[4] = "run"
	for i := 1; i <= 5; i++ {
		if b.effective(i) != stRun {
			t.Fatalf("section %d must run again after the command is lifted", i)
		}
	}
}
