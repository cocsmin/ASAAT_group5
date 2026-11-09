# Signal reference

The 14 signals of the alignment baseline, as implemented. Names are exact. Each
signal travels as Sparkplug B metrics named `<Signal>/<field>` in an `NDATA`
message (commands as `NCMD`). "Reliable" means QoS 1 with a message id: the
broker queues it for a receiver that is down and the receiver drops duplicates.
"State" means QoS 1 and repeated with every heartbeat, so a restarted receiver
learns the current value.

| # | Signal | From to | Kind | Fields |
|---|---|---|---|---|
| 1 | Production order and recipe targets | PLM to BAT | reliable | orderId, batchId, recipe, items (`ingredient:grams;...`), mixSeconds, doseTolPct, pansPerBatch, fillTarget, fillTol |
| 2 | Ingredient request and reservation result | BAT and PLM, both ways | reliable | request (true = BAT asks), orderId, batchId, items; reply: result (`confirmed`, sent after the withdrawal, 3 s after the reservation; `insufficient`, while the order is held; `staged`, when the warehouse does not answer), reason |
| 3 | Batter ready | BAT to THM | reliable | batchId, orderId, pans, fillTarget, fillTol |
| 4 | Depositing stopped / working again | THM to BAT | state | stopped, reason |
| 5 | Pan request | THM to FIN | reliable | message |
| 6 | Pan for washing | THM to FIN | reliable | panId, batchId, reason |
| 7 | Clean pan supply | FIN to THM | reliable | panId (empty when none), stock |
| 8 | Stop / resume pan admission | FIN to THM | state | stop, reason |
| 9 | Cooled cake | THM to FIN | reliable | panId, batchId, temp |
| 10 | Band state change | **BT** to THM, FIN, MON | state | S1 to S5, each `run`, `slow` or `halt` (`fault` is sent as `halt`); D1 to D5 with the detail incl. `fault` for the monitor |
| 11 | Pallet ready for shipment | FIN to PLM | reliable | palletId, cakes, batches |
| 12 | Log, warning, error and discard events | all to MON | outbox, QoS 1 | eventId, severity (`log`, `warning`, `error`, `discard`), code (UC or FC), reason, batchId, device, machineId, maintenance, time |
| 13 | Machine status (heartbeat) | all to MON | QoS 0, every T_hb | uptime, `dev.<device>` states, `g.<gauge>` counters |
| 14 | Repair confirmed | MON to the owning node | command, QoS 1 | machineId, repaired |

Operations messages that are not part of the 14 and not in the diagrams:
`Section command` (MON to BT, `NCMD`: node or section, state `run`/`slow`/`halt`, reason) and
`Target speed` (MON to BT, `NCMD`: speed in cm/s), which are the "sectionCommand" and "targetSpeed"
ports of the BT model; `Fault injection` (dashboard to a node, `NCMD`: code, count) and `Node Control`
(Sparkplug rebirth request, `NCMD`).

**Signals inside a system, between its stations** (not in the alignment baseline; they carry
what used to be an internal transition, see [stations.md](stations.md)); all reliable:

| Signal | From to | Fields |
|---|---|---|
| Batch to mixing | BAT-DOSE to BAT-MIX | batchId, orderId, mixSeconds, pans, fillTarget, fillTol |
| Batter to inspection | BAT-MIX to BAT-INSP | same |
| Batter rejected | BAT-INSP to BAT-MIX | same (another mixing cycle) |
| Batch result | BAT-MIX or BAT-INSP to BAT-DOSE | batchId, result (`handed` or `discarded`) |
| Pan to baking | THM-DEP to THM-BAKE | panId, batchId |
| Pan to tunnel exit | THM-BAKE to THM-EXIT | panId, batchId, off (temperature offset picked up in a faulty zone) |
| Pan to cooling | THM-EXIT to THM-COOL | panId, batchId, temp |
| Cake to inspection | FIN-DPN to FIN-QUA | panId, batchId |
| Cake to wrapping | FIN-QUA to FIN-WRAP | panId, batchId |
| Cake to cartoning | FIN-WRAP to FIN-PAL | panId, batchId |

The 14 signals above are sent by and to the station that does the work: "Batter ready"
BAT-INSP to THM-DEP, "Depositing stopped / working again" THM-DEP to BAT-DOSE and BAT-INSP,
"Cooled cake" THM-COOL to FIN-DPN, "Pan for washing" from THM-DEP, THM-BAKE (a pan found out of tolerance after a restart), THM-EXIT, THM-COOL and FIN-DPN to FIN-WASH, "Pan request" and "Clean pan supply"
THM-DEP and FIN-WASH, "Stop / resume pan admission" from THM-BAKE, THM-COOL, FIN-WRAP and
FIN-WASH to THM-DEP (and from THM-COOL to THM-EXIT: buffer full), "Pallet ready" FIN-PAL to
PLM-SHIP, "Production order" and the ingredient exchange between PLM-WH and BAT-DOSE.

Differences to the alignment baseline to settle with the group:
- PLM also sends the heartbeat. The baseline says PLM sends none; the
  monitor needs it to see a dead PLM.
- Band state carries all five sections; each receiver derives halt, slow-down or
  run from the sections it owns.
