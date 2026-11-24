//no deadlock
A[] not deadlock

//T-RC1: a silent node is known to MON within DEADAFTER (<= DETECT)
A[] (Node.Down && MON.Watching) imply t <= DEADAFTER

//T-RC1: and within the 15 s of THM-REQ-72
A[] (Node.Down && MON.Watching) imply t <= DETECT

//T-RC2: node back within 60 s
A[] (Env.Faulted && !Node.Online) imply t <= RESTORE

//MON never has to escalate
A[] not MON.Escalated

//a fault is reachable
E<> Node.Down

//recovery is reachable
E<> (Env.Faulted && Node.Online)

//every failure ends in recovery (leads-to)
Node.Down --> Node.Online
