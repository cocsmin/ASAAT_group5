//no deadlock
A[] not deadlock

//at most MAXOUT orders wait for BAT to ask their ingredients
A[] orders <= MAXOUT

//no burst of orders: never more than one per order cycle
A[] made <= 1

//BAT is confirmed only after a withdrawal has been booked
A[] gotConfirm imply withdrawn

//BAT gets its final answer (confirmed or staged) within RESTOCK + WITHDRAW = 18 s
A[] Bat.Waiting imply age <= 18

//a held order is resolved
Plm.Holding --> Plm.Idle

//communication resumes after a suspension
Plm.Suspended --> Plm.Idle

//BAT is always answered in the end
Bat.Waiting --> Bat.Ready

//after the warehouse answers again, the machine resumes within BREAKER + WTO (the suspension may have begun just before)
A[] (Plm.Suspended && !wmsDown) imply up <= BREAKER + WTO

//an order can be held
E<> Plm.Holding

//communication can be suspended
E<> Plm.Suspended

//BAT can be confirmed
E<> gotConfirm

//BAT can be confirmed although the stock was short and the warehouse silent
E<> (gotConfirm && Stock.Short && Wms.Back)
