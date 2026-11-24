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

//T-RC4 after the handoff: a batter is deposited at most P3 after approval
A[] Dep.Took imply h <= P3

//depositing discards a queued batter that waited past P3, and dosing is not kept waiting past LOST
A[] (Dep.Took || Dep.Expired) imply g <= LOST

//a batter is discarded at depositing at most DEPSTART + BLOCKMAX + DEPSTART (108 s) after approval
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

//FINDING: the state copy can be stale, a batter is handed to depositing while it is stopped
E<> (Batch.Handed && stopped)
