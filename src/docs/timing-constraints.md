# Exercise 8, D1: Timing constraints of the architecture

Draft for the report. Vocabulary from lecture 8: an **event** belongs to a structural
element (a port or a function block); an **event chain** links a *stimulus* to a
*response*; a **constraint** limits events or chains. Event-related constraints are
delay, execution time, repetition (periodic, sporadic, arbitrary) and order.
Event-chain constraints are reaction, input synchronization and output synchronization.

Values come from `internal/timing` and the station packages. Production times are
simulated seconds, supervision times are wall-clock seconds (`docs/assumptions.md`,
section 2). "Level" says whether the constraint belongs to the analysis level (the
requirement in the report) or the design level (the value chosen in the implementation).

## 1. Event chains

| Chain | Stimulus | Response | Systems on the chain |
|---|---|---|---|
| EC1 Failure detection | a container stops (crash or freeze) | MON has the death certificate (NDEATH) and raises an event | any node, broker, MON |
| EC2 Recovery | a container stops | the container is reborn (NBIRTH) and production resumes | any node, Docker, MON |
| EC3 Supervision | a node publishes a heartbeat | MON sees it before the next one is due | any node, MON |
| EC4 Order to cake | PLM releases an order | the cakes of that order are shipped | PLM, BAT, THM, FIN, BT, PLM-SHIP |
| EC5 Band stop | a band section halts | every station upstream stops taking new pans | BT, THM, FIN |
| EC6 Cooling failure | a cooling failure is reported | the cakes are re-cooled or refused (UC10) | THM-COOL, FIN-DPN |
| EC7 Batter handoff | the batter is approved at inspection | the batter is handed to depositing ("Batter ready"), or discarded; there is no acknowledgement, BAT does not see when depositing starts | BAT-INSP, THM-DEP |
| EC8 Handover | a station hands a pan or cake document to the next station | the next station has taken it up | any two neighbouring stations |

## 2. Event-chain constraints

| Id | Type | Chain | Constraint | Value | Level | Source |
|---|---|---|---|---|---|---|
| T-RC1 | Reaction | EC1 | detected within 3 heartbeat intervals | at most 15 s; the broker's keep-alive finds a frozen node after 7.5 s; a crash at once (measured at most 0.43 s) | analysis and design | THM-REQ-72, `timing.DetectLimit`, `timing.KeepAlive` |
| T-RC2 | Reaction | EC2 | back in normal operation after the cause is removed | at most 60 s; a container that has not restarted after 45 s goes to Maintenance | analysis and design | THM-REQ-73, `timing.RestoreLimit`, `timing.MonDeadAfter` |
| T-RC3 | Reaction | EC5 | stop new pans after a band halt | at most 1 s | analysis | THM-REQ-76 and -78 (not measured) |
| T-RC4 | Reaction | EC7 | batter that waits for depositing to work again is discarded | after 25 s (parameter P3), looked at once a second, so after 26 s; counted before the handoff only, after it nothing limits the wait (`team-review.md`, section 3) | design | `timing.BatterWait`, FC7, `bat.handOff` |
| T-RC5 | Reaction | EC6 | decide refuse or re-cool after a cooling failure | 30 min in the plant, 40 s in the simulation (factor 45) | analysis and design | UC10, `timing.BufferWait` |
| T-RC6 | Reaction | EC4 | a batch whose result does not come back is given up | after 120 s, which is longer than mixing plus inspection plus P3 (unit test) | design | `batchLostAfter`, FC27 |
| T-RC7 | Reaction | EC8 | a station takes up a handed-over document that the message did not deliver | after 3 s grace, checked every 5 s | design | `station.HandoverGrace`, `station.ReconcileEvery` |
| T-RC8 | Reaction | EC4 | an order without an ingredient request is sent again | after 40 s | design | `timing.OrderResend` |
| T-IS1 | Input synchronization | EC5 | the stations on one band section must see the same halt state | all five sections stop together; sections restart 1.5 s apart | design | `bt` package |
| T-OS1 | Output synchronization | EC1 | all of MON, the dashboard and the event log learn of one failure | within one MQTT delivery, no longer than the detection time T-RC1 | design | not measured |

## 3. Event-related constraints

| Id | Type | Event | Constraint | Value | Level | Source |
|---|---|---|---|---|---|---|
| T-P1 | Periodic | heartbeat of every node | one every T_hb | 5 s, the same for every system | analysis | `timing.Heartbeat` |
| T-P2 | Periodic | MQTT keep-alive | one every 5 s | 5 s | design | `timing.KeepAlive` |
| T-P3 | Periodic | state snapshot of a node | one every 3 s | 3 s | design | `timing.StateSnapshot` |
| T-P4 | Periodic | station reconcile pass | one every 5 s | 5 s | design | `station.ReconcileEvery` |
| T-P5 | Periodic | PLM order | one order per cycle | 22 s (`ORDER_SECONDS`, default in `internal/plm/plm.go` and `docker-compose.yml`); estimated, the plant value is unknown | design | `plm` package |
| T-SP1 | Sporadic | a pan enters depositing | at least one pan time apart | 1 s, one pan per second | design | `thm.depositTime` |
| T-SP2 | Sporadic | failure case FC1 to FC28 | arbitrary in the plant; random in the simulation with a rate per hour | about 32 per hour in total at `FAULT_RATE=1` | design | `model.FaultCatalog`, `PerHour` |
| T-EX1 | Execution time | dosing one ingredient | 1.2 s | design | `bat` package |
| T-EX2 | Execution time | inspection | 3 s | design | `bat.inspectionTime` |
| T-EX3 | Execution time | depositing a pan | 1 s | design | `thm.depositTime` |
| T-EX4 | Execution time | each of three baking zones | 2 s | design | `thm.zoneTransit` |
| T-EX5 | Execution time | cooling | 4 s | design | `thm.coolingTime` |
| T-EX6 | Execution time | depanning, inspection, wrapping, cartoning | 1 s each | design | `fin` package |
| T-EX7 | Execution time | speed or temperature adjustment | T_adjust 4 s | design | `thm.tAdjust` |
| T-D1 | Delay | database call | at most 3 s, then the station carries on | design | `timing.DBTimeout` |
| T-D2 | Delay | event delivery | retried every 1 s until acknowledged | design | `timing.EventRetry` |
| T-D3 | Delay | request to the warehouse (FC2) | at most 2 s; the circuit breaker stays open for 15 s | design | `timing.WarehouseTimeout`, `timing.BreakerOpen` |
| T-D4 | Delay | ingredient request repeated | after 20 s | design | `timing.IngredientRetry` |
| T-OR1 | Order | handoffs | a handoff signal for a pan arrives after its document is stored, never before | design | `signals.md` |
| T-OR2 | Order | MON birth and halt release | a section is released only after the birth of its owner | design | `mon.onBirth` |

## 4. Relations that must hold between the values

These are checked by `internal/timing/timing_test.go` and `internal/bat/bat_test.go`:

- keep-alive detection (1.5 x 5 s = 7.5 s) is below the 15 s detection limit;
- the 15 s limit equals three heartbeats;
- the 45 s escalation is below the 60 s recovery limit;
- `batchLostAfter` (120 s) is longer than mixing plus inspection plus P3;
- the compression factor is 45 (30 min to 40 s).

## 5. Requirement list update

New or changed requirements, to add to the group's list. The numbers are the group's to
confirm.

| New id | Text | Based on |
|---|---|---|
| TIM-01 | Every node shall send a heartbeat every 5 s. | T-P1 |
| TIM-02 | MON shall detect a stopped node within 15 s. | T-RC1, THM-REQ-72 |
| TIM-03 | A stopped container shall be back in normal operation within 60 s after the cause is removed, and escalated to Maintenance after 45 s without a restart. | T-RC2, THM-REQ-73 |
| TIM-04 | Batter that has waited 25 s for depositing to work again shall be discarded. | T-RC4 |
| TIM-05 | A batch that has not come back after 120 s shall be discarded. | T-RC6 |
| TIM-06 | A handed-over document that no message announced shall be taken up within 8 s (3 s grace plus one 5 s pass). | T-RC7 |
| TIM-07 | A database call shall never block a station for more than 3 s. | T-D1 |
| TIM-08 | After a band halt every upstream station shall stop taking new pans within 1 s. | T-RC3 |

## 6. What the experiment says

Detection at most 0.43 s (crash) and 7.06 s (freeze), recovery at most 1.19 s, 270 runs
(`experiment.md`). T-RC3 and T-OS1 are not measured. T-RC4 holds before the handoff; the UPPAAL
batter model shows a batter can be deposited up to 70 s after approval (estimated stop time),
see `team-review.md` section 3.
