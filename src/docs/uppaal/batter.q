//no deadlock
A[] not deadlock

//T-RC4: before the handoff the batter holds at most P3 + POLL
A[] Batch.Holding imply Batch.x <= P3 + POLL

//BAT's side of the batch is finished within MIX + INSP + P3 + POLL
A[] (Batch.Mixing || Batch.Inspecting || Batch.Holding) imply g <= MIX + INSP + P3 + POLL

//T-RC6: and so well before the LOST limit
A[] (Batch.Mixing || Batch.Inspecting || Batch.Holding) imply g <= LOST

//T-RC6: a batter is deposited before dosing gives the batch up
A[] Dep.Took imply g <= LOST

//from approval to deposit at most DEPSTART + BLOCKMAX + DEPSTART (108 s), more than P3
A[] Dep.Took imply h <= 2 * DEPSTART + BLOCKMAX

//deposited and discarded exclude each other
A[] not (Batch.Discarded && deposited)

//the discard path is reachable
E<> Batch.Discarded

//the deposit path is reachable
E<> Dep.Took

//FINDING: the state copy can be stale, a batter is handed to depositing while it is stopped
E<> (Batch.Handed && stopped)

//FINDING: after the handoff nothing limits the wait, a batter is deposited more than P3 + POLL after approval
E<> (Dep.Took && h > P3 + POLL)
