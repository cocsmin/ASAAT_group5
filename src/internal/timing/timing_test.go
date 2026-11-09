package timing

import (
	"testing"
	"time"
)

// The relations that the systems rely on. A change to one value that breaks one
// of them fails here instead of showing up as a stalled line.
func TestRelations(t *testing.T) {
	check := func(ok bool, what string) {
		t.Helper()
		if !ok {
			t.Error(what)
		}
	}
	check(WatchdogMisses*Heartbeat <= DetectLimit, "silent detection (misses x heartbeat) must be within THM-REQ-72")
	check(KeepAlive*3/2 <= DetectLimit, "the broker's dead-node detection must be within THM-REQ-72")
	check(EventRetry <= Heartbeat, "events must be retried at least as often as the heartbeat")
	check(StateSnapshot <= Heartbeat, "saved state must be at least as fresh as the heartbeat")
	check(MonDeadAfter < RestoreLimit, "an unrestarted container must be escalated before THM-REQ-73 is broken")
	check(MonDeadAfter > DetectLimit, "give Docker more time than detection before escalating")
	check(OrderResend >= 2*IngredientRetry, "PLM must not resend an order before BAT had two chances to ask")
	check(WarehouseTimeout < DBTimeout, "the warehouse circuit breaker must trip before the database timeout")
	check(BatterWait > KeepAlive*3/2, "batter must survive the time a restarted THM needs to be noticed")
	check(BufferWait*dur(Compression) == RealBufferWait, "compression factor must reproduce 30 minutes exactly")
}

func dur(n int) time.Duration { return time.Duration(n) }
