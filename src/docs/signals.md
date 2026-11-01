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
| 2 | Ingredient request and reservation result | BAT and PLM, both ways | reliable | request (true = BAT asks), orderId, batchId, items; reply: result (`confirmed`, `insufficient`, `staged`), reason |
| 3 | Batter ready | BAT to THM | reliable | batchId, orderId, pans, fillTarget, fillTol |
| 4 | Depositing stopped / working again | THM to BAT | state | stopped, reason |
| 5 | Pan request | THM to FIN | reliable | message |
| 6 | Pan for washing | THM to FIN | reliable | panId, batchId, reason |
| 7 | Clean pan supply | FIN to THM | reliable | panId (empty when none), stock |
| 8 | Stop / resume pan admission | FIN to THM | state | stop, reason |
| 9 | Cooled cake | THM to FIN | reliable | panId, batchId, temp |
| 10 | Band state change | MON to THM, FIN | state | S1 to S5, each `run`, `slow` or `halt` |
| 11 | Pallet ready for shipment | FIN to PLM | reliable | palletId, cakes, batches |
| 12 | Log, warning, error and discard events | all to MON | outbox, QoS 1 | eventId, severity (`log`, `warning`, `error`, `discard`), code (UC or FC), reason, batchId, device, machineId, maintenance, time |
| 13 | Machine status (heartbeat) | all to MON | QoS 0, every T_hb | uptime, `dev.<device>` states, `g.<gauge>` counters |
| 14 | Repair confirmed | MON to the owning node | command, QoS 1 | machineId, repaired |

Operations messages that are not part of the 14 and not in the diagrams:
`Fault injection` (dashboard to a node, `NCMD`: code, count) and `Node Control`
(Sparkplug rebirth request, `NCMD`).

Differences to the alignment baseline to settle with the group:
- PLM also sends the heartbeat. The baseline says PLM sends none; the
  monitor needs it to see a dead PLM.
- Band state carries all five sections; each receiver derives halt, slow-down or
  run from the sections it owns.
