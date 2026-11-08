//no deadlock
A[] not deadlock

//the halt spreads upstream within N hops of 1 s
A[] (faulted && !released && t > N * HOP) imply nstopped == hk + 1

//a section passes the halt on within 1 s
A[] forall (i : id_t) Section(i).Pass imply Section(i).x <= HOP

//never released before the owner is back
A[] released imply ownerUp[hk]

//after the release nothing stays stopped
A[] released imply nstopped == 0

//no more stopped sections than are upstream
A[] nstopped <= hk + 1

//release is reachable
E<> released
