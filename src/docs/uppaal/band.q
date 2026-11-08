//no deadlock
A[] not deadlock

//the halt spreads upstream within N hops of 1 s
A[] (faulted && !released && t > N * HOP) imply nstopped == hk + 1

//a section never stays halted longer than 1 s before it passes the halt on
A[] forall (i : id_t) Section(i).Halted imply Section(i).x <= HOP

//never released before the owner is back
A[] released imply ownerUp[hk]

//after the release nothing stays stopped
A[] released imply nstopped == 0

//no more stopped sections than are upstream
A[] nstopped <= hk + 1

//release is reachable
E<> released
