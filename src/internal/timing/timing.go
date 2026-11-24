// Package timing holds every time value that more than one system depends on,
// in one place, so they cannot drift apart. timing_test.go checks the relations
// between them. Production durations (dosing, baking, washing...) stay in the
// system that owns them and are *simulated* time (see sim.Scale); the values
// here that supervise the line (heartbeat, keep-alive, timeouts) are wall-clock.
package timing

import "time"

const (
	// Supervision (wall-clock).
	Heartbeat      = 5 * time.Second  // T_hb, one value for every system
	WatchdogMisses = 3                // heartbeats missed before a node counts as silent (THM-REQ-72)
	DetectLimit    = 15 * time.Second // THM-REQ-72: a stopped controller is detected within this
	KeepAlive      = 5 * time.Second  // MQTT keep-alive: the broker declares a node dead after 1.5 x this
	RestoreLimit   = 60 * time.Second // THM-REQ-73: back to normal within this after a repair
	MonDeadAfter   = 45 * time.Second // a container that has not restarted by itself after this is escalated
	DBTimeout      = 3 * time.Second  // every database call, so a station carries on while a database is off
	EventRetry     = 1 * time.Second  // outbox delivery attempts
	StateSnapshot  = 3 * time.Second  // saved state of every node

	// Handoffs between systems (simulated time).
	IngredientRetry  = 20 * time.Second // BAT repeats an unanswered ingredient request
	OrderResend      = 40 * time.Second // PLM repeats an order BAT never asked ingredients for
	BatterWait       = 25 * time.Second // P3: batter that waited this long for depositing is discarded (FC7)
	WarehouseTimeout = 2 * time.Second  // PLM request to the warehouse (FC2)
	BreakerOpen      = 15 * time.Second // PLM circuit breaker stays open

	// UC10: 30 minutes between cooling failure and repair decide refuse or recool.
	// The simulation compresses them; Compression is the documented factor.
	RealBufferWait = 30 * time.Minute
	BufferWait     = 40 * time.Second
	Compression    = int(RealBufferWait / BufferWait) // 45: one simulated second stands for 45 real seconds
)
