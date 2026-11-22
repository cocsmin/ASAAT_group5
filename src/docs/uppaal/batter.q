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

//T-RC6: a batter is deposited at the latest 2 * (MIX + INSP) + 2 * DEPSTART + BLOCKMAX = 126 s after the batch was started (tight: 125 fails)
A[] Dep.Took imply g <= 2 * (MIX + INSP) + 2 * DEPSTART + BLOCKMAX

//FINDING: T-RC6 is broken by a single rejection. 126 s is past dosing's LOST limit, so a batter can be deposited for a batch dosing has already given up (FC27) and replaced. Set rejectDone = true in the declarations and this becomes NOT satisfied, so the rejection is the whole cause
E<> (Dep.Took && g > LOST)

//from approval to deposit at most DEPSTART + BLOCKMAX + DEPSTART (108 s), more than T_batter
A[] Dep.Took imply h <= 2 * DEPSTART + BLOCKMAX

//deposited and discarded exclude each other
A[] not (Batch.Discarded && deposited)

//the discard path is reachable
E<> Batch.Discarded

//the deposit path is reachable
E<> Dep.Took

//every inspected batch ends, handed over or discarded
Batch.Inspecting --> (Batch.Handed || Batch.Discarded)

//a queued batter is always taken up in the end
Dep.OpenQ --> Dep.Took

//FINDING: the state copy can be stale, a batter is handed to depositing at a moment when it is really stopped (unreachable if the state signal arrives without delay)
E<> handedWhileStopped

//FINDING: after the handoff nothing limits the wait, a batter is deposited more than T_batter + POLL after approval (the exact maximum is 108 s: A[] Dep.Took imply h <= 107 fails, h <= 108 holds)
E<> (Dep.Took && h > T_batter + POLL)

//LIMIT: both stations can be frozen for good after a discard, which `not deadlock` above does not see
E<> (Batch.Discarded && Dep.Open && faultDone)

//LIMIT: and after a deposit
E<> (Batch.Handed && Dep.Done)
