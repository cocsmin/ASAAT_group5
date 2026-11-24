package scenario

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// longRunning scenarios are left out of "run all" (they take as long as you ask them to).
var longRunning = map[string]bool{"availability": true}

// lineSilence is how long the line may ship nothing before it counts as not producing: a pallet
// of 24 cakes leaves about every 25 s, so 75 s without one is a stop.
const lineSilence = 75 * time.Second

// availability watches the factory for Options.Duration while it fails by itself (start the
// stack with FAULT_RATE and CREW_SECONDS, see docs/experiment.md) and reports what an operator
// would ask: how long was every container up, how long did the line produce, how many failures
// of which kind happened, how long did a repair take, and was a pan lost.
func availability(e *env) error {
	dur := e.duration
	if dur <= 0 {
		dur = 30 * time.Minute
	}
	if err := e.settle(); err != nil {
		return err
	}
	pans, _ := e.pans()
	since := e.lastEventID()
	st, err := e.state()
	if err != nil {
		return err
	}
	up := map[string]int{}
	downSince := map[string]time.Time{}
	var outages []time.Duration
	var ids []string
	for _, n := range st.Stations {
		ids = append(ids, n.ID)
	}
	ticketSeen := map[int64]time.Time{}
	var repairs []time.Duration
	codes := map[string]int{}
	shipped0 := e.gauge("PLM", "cakes.shipped")
	lastShipped, lastShipAt := shipped0, time.Now()
	total, productive, noData := 0, 0, 0
	t0 := time.Now()
	nextReport := t0.Add(5 * time.Minute)
	lastEvents := time.Now()

	e.logf("watching the factory for %s", dur.Round(time.Second))
	for time.Since(t0) < dur {
		select {
		case <-e.cancel:
			return ErrStopped
		case <-time.After(time.Second):
		}
		total++
		now := time.Now()
		s, err := e.state()
		if err != nil {
			noData++
			continue
		}
		for _, n := range s.Stations {
			if n.Status == "online" {
				up[n.ID]++
				if t, down := downSince[n.ID]; down {
					outages = append(outages, now.Sub(t))
					delete(downSince, n.ID)
				}
			} else if _, down := downSince[n.ID]; !down {
				downSince[n.ID] = now
			}
		}
		for _, tk := range s.Tickets {
			if tk.Status == "open" {
				if _, ok := ticketSeen[tk.ID]; !ok {
					ticketSeen[tk.ID] = now
				}
			} else if t, ok := ticketSeen[tk.ID]; ok {
				repairs = append(repairs, now.Sub(t))
				delete(ticketSeen, tk.ID)
			}
		}
		var shipped int64
		for _, n := range s.Nodes {
			if n.ID == "PLM" {
				shipped = n.Gauges["cakes.shipped"]
			}
		}
		if shipped > lastShipped {
			lastShipped, lastShipAt = shipped, now
		}
		if now.Sub(lastShipAt) < lineSilence {
			productive++
		}
		if now.Sub(lastEvents) >= 10*time.Second { // count the failures as they are logged
			lastEvents = now
			if ev, err := e.events(since); err == nil {
				for _, x := range ev {
					if x.ID > since {
						since = x.ID
					}
					if x.Severity == "error" || x.Severity == "discard" || (x.Severity == "warning" && x.Code != "UC2") {
						codes[x.Code+" "+x.Severity]++
					}
				}
			}
		}
		if now.After(nextReport) {
			nextReport = now.Add(5 * time.Minute)
			e.logf("%s: line producing %.1f %%, %d cakes shipped, %d repairs open", time.Since(t0).Round(time.Second),
				100*float64(productive)/float64(total), e.gauge("PLM", "cakes.shipped")-shipped0, len(ticketSeen))
		}
	}

	// report
	sort.Strings(ids)
	var sum float64
	worst, worstID := 100.0, ""
	for _, id := range ids {
		a := 100 * float64(up[id]) / float64(total)
		sum += a
		if a < worst {
			worst, worstID = a, id
		}
	}
	mean := 0.0
	if len(ids) > 0 {
		mean = sum / float64(len(ids))
	}
	lineAv := 100 * float64(productive) / float64(total)
	shipped := e.gauge("PLM", "cakes.shipped") - shipped0
	hours := time.Since(t0).Hours()
	e.logf("%d s watched (%d s without an answer from the dashboard)", total, noData)
	e.logf("container availability: mean %.3f %%, worst %.3f %% (%s)", mean, worst, worstID)
	e.logf("line availability (a pallet leaves at least every %s): %.2f %%", lineSilence, lineAv)
	e.logf("%d outages of a container, mean %.1f s, longest %.1f s", len(outages), meanDur(outages), maxDur(outages))
	e.logf("%d repairs by the crew, mean %.0f s from the ticket to the confirmed repair", len(repairs), meanDur(repairs))
	e.logf("%.0f cakes per hour (at most %.0f: one pan per second)", float64(shipped)/hours, 3600.0)
	var list []string
	for k, v := range codes {
		list = append(list, fmt.Sprintf("%s x%d", k, v))
	}
	sort.Strings(list)
	e.logf("events by code: %s", strings.Join(list, ", "))
	e.rec("availability_pct", mean)
	e.rec("line_availability_pct", lineAv)
	e.rec("outages", float64(len(outages)))
	e.rec("mttr_s", meanDur(outages))
	e.rec("cakes_per_hour", float64(shipped)/hours)

	total2, by := e.pans()
	e.rec("pans_lost", float64(pans-total2))
	e.logf("pans: %d of %d in the loop %v", total2, pans, by)
	if total2 != pans {
		return fmt.Errorf("pan count changed: %d, expected %d", total2, pans)
	}
	return nil
}

func meanDur(d []time.Duration) float64 {
	if len(d) == 0 {
		return 0
	}
	var s time.Duration
	for _, x := range d {
		s += x
	}
	return s.Seconds() / float64(len(d))
}

func maxDur(d []time.Duration) float64 {
	var m time.Duration
	for _, x := range d {
		if x > m {
			m = x
		}
	}
	return m.Seconds()
}
