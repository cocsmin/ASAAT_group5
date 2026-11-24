//no deadlock (weak here: Copy never stops, so `deadlock` is unreachable whatever the stations do, see the two LIMIT queries)
A[] not deadlock

//T-RC4: a discarded batter waited exactly T_batter + POLL = 26 polls, derived from the loop and not asserted anywhere (tight: the same query with <= 25 fails). Counted on `waited`, not on a clock: Discarded is terminal, so any clock keeps running after the batch has ended
A[] Batch.Discarded imply Batch.waited <= T_batter + POLL

//T-RC4: and it never waits longer than that before the handoff
A[] Batch.Holding imply h <= T_batter + POLL

//BAT's side of the batch is finished within 2 * (MIX + INSP) + T_batter + POLL = 44 s: one mixing cycle, one rejected cycle, and the full hold
A[] (Batch.Mixing || Batch.Inspecting || Batch.Holding) imply g <= 2 * (MIX + INSP) + T_batter + POLL

//T-RC6: and so well before the LOST limit
A[] (Batch.Mixing || Batch.Inspecting || Batch.Holding) imply g <= LOST

//T-RC4 after the handoff: a batter is deposited at most T_batter after approval
A[] Dep.Took imply h <= T_batter

//T-RC6: a batter is deposited before dosing gives the batch up, also with one rejection (was a FINDING before 24 Nov: 126 s)
A[] Dep.Took imply g <= LOST

//a batter that waited past T_batter is discarded at depositing at the latest DEPSTART + BLOCKMAX + DEPSTART = 108 s after approval (exact: h <= 107 fails)
A[] Dep.Expired imply h <= 2 * DEPSTART + BLOCKMAX

//depositing and the discard at depositing exclude each other
A[] not (Dep.Expired && deposited)

//the discard at depositing is reachable
E<> Dep.Expired

//deposited and discarded exclude each other
A[] not (Batch.Discarded && deposited)

//the discard path is reachable
E<> Batch.Discarded

//the deposit path is reachable
E<> Dep.Took

//every inspected batch ends, handed over or discarded
Batch.Inspecting --> (Batch.Handed || Batch.Discarded)

//a queued batter is always taken up or discarded in the end
Dep.OpenQ --> (Dep.Took || Dep.Expired)

//FINDING: the state copy can be stale, a batter is handed to depositing at a moment when it is really stopped (unreachable if the state signal arrives without delay)
E<> handedWhileStopped

//LIMIT: both stations can be frozen for good after a discard, which `not deadlock` above does not see
E<> (Batch.Discarded && Dep.Open && faultDone)

//LIMIT: and after a deposit
E<> (Batch.Handed && Dep.Done)
