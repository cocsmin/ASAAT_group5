//no deadlock
A[] not deadlock

//T-RC4: batter never waits longer than P3
A[] Batch.Waiting imply Batch.x <= P3

//T-RC6: the batch is finished well before the LOST limit
A[] (Batch.Mixing || Batch.Inspecting || Batch.Waiting) imply g <= LOST

//the longest path is MIX + INSP + P3
A[] (Batch.Mixing || Batch.Inspecting || Batch.Waiting) imply g <= MIX + INSP + P3

//deposited and discarded exclude each other
A[] not (Batch.Deposited && Batch.Discarded)

//the discard path is reachable
E<> Batch.Discarded

//the deposit path is reachable
E<> Batch.Deposited
