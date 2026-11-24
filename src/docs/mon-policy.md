# What the monitor does with what it sees (policy of `internal/mon`)

This is the policy table that was missing for the UC2 error flow. It is read from the code (`mon.go`), so
it is the implemented policy; changing a row means changing `mon.go`.

| What MON sees | What MON does | Code |
|---|---|---|
| Birth certificate (NBIRTH) of a node | the node is `online`; if it had halted band sections they are released (the first birth after a death or a silent period counts); a restart is counted | `onBirth` |
| Death certificate (NDEATH) of a node | the node is `dead`; if it owns a band section MON tells BT to halt that section and every section before it ("Section command", `halt`); after 45 s without a restart an error event FC27 and a Maintenance ticket for the container | `onDeath`, `watchdog` |
| No heartbeat for 3 x T_hb = 15 s while the broker still shows the node connected | the node is `silent`; error event FC28, Maintenance ticket for the controller, band sections halted as for a death | `watchdog` |
| Heartbeat after silence | the node is `online` again, the sections are released, "marked operational" is logged | `onHeartbeat` |
| Event with severity `log`, `warning` | stored (400 in memory, all in SQL), shown on the dashboard | `handleEvent` |
| Event with severity `discard` | stored and counted per system | `handleEvent` |
| Event with severity `error` and a machine id | stored and a Maintenance ticket for that machine (one open ticket per machine) | `handleEvent`, `openTicket` |
| "Repair confirmed" by Maintenance (dashboard) | the ticket is closed and the command goes to the node that owns the machine; the section halted for it is released | `Repair`, `RepairMachine` |
| Band state change from BT | the five section states are kept for the dashboard | `onBand` |
| Band speed wrong (FC20) | BT corrects the speed; if that fails an error event and a ticket for the band drive | `bt` package, `handleEvent` |
| The monitor itself is down | the nodes keep sending events (queued, QoS 1) and deliver them when MON is back; PLM releases no order and BAT-DOSE starts no batch (S4); the line finishes what it has | `node.HostOnline` |
| SQL Server is down | MON queues its writes and retries; health detection does not depend on SQL | `dbDo` |

**The general rule of UC2** (any error may need a halt) is implemented as: only a station that is dead or silent
halts the sections it owns, and a section fault of BT (FC19) halts the sections before it. Other errors are
handled by the system that raised them (discard, retry, lock-out) and reach MON as events and tickets, not as
a halt. Reason: halting the band for a wrong fill weight would stop 15 stations to protect one pan.
