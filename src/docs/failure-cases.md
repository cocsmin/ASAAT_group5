# Failure cases: what is implemented and how to trigger it

All 28 failure cases of the report (FC1 to FC28) are implemented, together with the
use-case alternatives UC4, UC5, UC12 and UC14. "Button" is the *Inject a failure
case* panel of the dashboard (the code is on the button); "scenario" is
`go run ./cmd/scenario <name>` or the *Test scenarios* card. Every case below is
run by a scenario and passes against the real containers.

| FC | Failure | Resolved by (report) | Where | Trigger |
|---|---|---|---|---|
| FC1 | Warehouse: not enough ingredients | System, Logistics notified | PLM, BAT | button PLM FC1; `faults-system` |
| FC2 | Warehouse system not responding | System (circuit breaker) | PLM | button PLM FC2; `faults-system` |
| FC3 | Dosing: too little of an ingredient | System | BAT | button BAT FC3; `faults-system` |
| FC4 | Dosing: too much of an ingredient | System | BAT | button BAT FC4; `faults-system` |
| FC5 | Dosing fails several times in a row | Maintenance | BAT | button BAT FC5 (3 failures); `faults-maintenance` |
| FC6 | Mixer breaks | Maintenance | BAT | button BAT FC6; `faults-maintenance` |
| FC7 | Batter waits too long (depositing stopped) | System | BAT, THM | button THM FC7 (depositing jam); `faults-system` |
| FC8 | Depositing: no pan | System | THM | button THM FC8; `faults-system` |
| FC9 | Depositing: wrong fill weight | System | THM | button THM FC9; `faults-system` |
| FC10 | Wrong fill weight several times in a row | Maintenance | THM | button THM FC10 (3 fills); `faults-maintenance` |
| FC11 | Baking tunnel: zone temperature wrong | Maintenance | THM | button THM FC11; `faults-maintenance` |
| FC12 | Tunnel exit: cake under- or overcooked | System | THM | button THM FC12; `faults-system` |
| FC13 | Tunnel exit: heat camera broken | Maintenance | THM | button THM FC13; `faults-maintenance` |
| FC14 | Cooling: cake still too hot | System | THM | button THM FC14; `faults-system` |
| FC15 | Cooling stops working | Maintenance | THM | button THM FC15; `faults-maintenance`, `cooling-chain` |
| FC16 | Cooling buffer: cooling is fixed | System | THM | follows the repair of FC15; `cooling-chain` |
| FC17 | Cooling buffer: cake waited too long | System | THM | follows FC15 after the waiting limit; `cooling-chain` |
| FC18 | Cooling buffer full | Maintenance (cooling fixed) | THM | follows FC15 when 8 cakes are held; `cooling-chain` |
| FC19 | Band section stops | Maintenance | BT | button BT FC19; `faults-maintenance` |
| FC20 | Band: wrong speed | System, then Maintenance | BT | button BT FC20 (corrected) and "correction fails" (ticket); `faults-maintenance` |
| FC21 | Depanning: cake stuck or broken | System | FIN | button FIN FC21; `faults-system` |
| FC22 | Depanning machine breaks | Maintenance | FIN | button FIN FC22; `faults-maintenance` |
| FC23 | Pan washing: washing station breaks | Maintenance | FIN | button FIN FC23; `faults-maintenance` |
| FC24 | Quality inspection: defect, weight or metal | System; the same check failing repeatedly: Maintenance | FIN | button FIN FC24 and "same check fails 3 times"; `faults-system`, `faults-maintenance` |
| FC25 | Wrapping station breaks | Maintenance | FIN | button FIN FC25; `faults-maintenance` |
| FC26 | Cartoning: wrong case weight | System | FIN | button FIN FC26; `faults-system` |
| FC27 | Controller (Docker container) crashes | System (Docker restarts it) | all | button FC27 on each system; `crash-plm-wh`, `crash-plm-ship`, `crash-bat-dose`, `crash-bat-mix`, `crash-bat-insp`, `crash-thm-dep`, `crash-thm-bake`, `crash-thm-exit`, `crash-thm-cool`, `crash-fin-dpn`, `crash-fin-qua`, `crash-fin-wrap`, `crash-fin-pal`, `crash-fin-wash`, `crash-bt` |
| FC28 | Any machine stops responding | Maintenance | all | button FC28 on each system; `freeze-thm-bake` (frozen process) |

Use-case alternatives: UC4 (the inspector rejects the batter, `faults-system`),
UC5 (Weight-Checker not calibrated, `faults-system`), UC12 (damaged pan, and the
catch-up washing after a washing fault, `faults-system`, `faults-maintenance`),
UC14 (wrap or carton defective, `faults-system`), UC10 (warm cakes pile up: the FC15
chain, `cooling-chain`), UC2 (monitor flows, band halt and slow-down).

## Where the simulation is simpler than the report

- **FC28** is modelled per system controller, not per individual machine: the
  controller stops answering and the monitor marks it faulty and halts its band
  sections. Individual devices (a sensor, a diverter) report their own state, but
  only the faults above make them fail.
- **FC13**: the broken heat camera is detected immediately (the report's T_resp is
  not used).
- **FC16** is shown for the late repair (more than 30 minutes) in `cooling-chain`;
  the in-time repair, where buffered cakes are released and measured, is
  implemented (`thm.releaseLoop`) but no scenario waits for it.
- **FC20** "System, then Maintenance": the system correction is simulated as a fixed
  10 s timer; there is no real speed sensor.
- Time is compressed: the 30 minutes of UC10 are 40 simulated seconds.
