# TODO

The discussion list for the team (time constraints, decisions taken without the group, known bugs, odd things) is in [docs/team-review.md](docs/team-review.md).

Everything that is not done, not verified, or not mine to decide. Items marked
**[person]** need a person or a tool that the implementation session did not have.

## Diagrams to fix (Papyrus models, **[person]**: the alignment owner)

The implementation follows the diagrams where they exist. These are the places
where the diagrams are missing, wrong, or behind the implementation.

1. **Cake Factory IBD** is empty. Add the message bus (Sparkplug B on an MQTT
   broker) and the MON part, and connect the frame ports of the slices to the bus

2. **BAT frame port "Fault event"** is not in the signal table. Make it internal
   (remove the frame port) or add it as a 15th signal.
3. **MON has no document and no diagrams.** Needed: blocks, IBD and state machines
   for the node watchdog (online, silent, dead), the band sections (run, slow,
   halt, fault) and the ticket lifecycle (open, repaired). The code in
   `internal/mon` can be the starting point.
4. **PLM has no state machines.** Needed: order lifecycle (released, reserved,
   held), the circuit breaker toward the warehouse (closed, open, test request),
   shipments.
5. **Rule "production may not run without the Monitor" (UC2) is not in any state
   machine.** Add a stop reason "monitor offline" to THM SM1 (admission), a hold
   to BAT Mixing and Handoff, and a gate to PLM order release.
6. **Container recovery (FC27) is modelled in no state machine.** THM, FIN and BAT
   need the behaviour "restart, reload saved state, take up cakes and pans again
   from the stored documents".
7. **BAT Mixing and Handoff** has `batchCollected`, which is not one of the 14
   signals. **DECIDED (proposed 2026-11-09, group to confirm): option B, remove it.**
   The code (`internal/bat/insp.go`, `handOff`), the UPPAAL batter model
   (`docs/uppaal/batter.xml`) and the documents already follow B; only Papyrus changes.
   Why: the 14-signal interface and the measured experiment stay valid, and a lost
   "Batter ready" is already covered (QoS 1, persistent session, saved queue in THM-DEP,
   duplicate filter, the 120 s timer of dosing). See `docs/team-review.md` section 1, item 2.
   **To do in Papyrus (BAT state machine "Mixing and Handoff"):**
   - delete the event/trigger `batchCollected` and the state or transition that waits for it;
   - after the inspector approves the batter, check the depositing state (the last
     "Depositing stopped / working again" received): if working, send "Batter ready" and
     the batch is done for BAT;
   - if stopped, enter a state `Holding` with a time event `after(P3)`, P3 = 25 s
     (`timing.BatterWait`); on "working again" leave it and send "Batter ready"; when the
     timer ends, discard the batter (event FC7) and report "Batch result" = discarded;
   - keep the 14 signals and the ports of BAT and THM as they are (no new port).
   **Open, optional (code):** P3 is checked before the handoff only; nothing in THM-DEP
   discards a queued batter (found with UPPAAL, `docs/team-review.md` section 3, item 11).
   If the group wants it fixed: add the already-waited time to "Batter ready" and let
   THM-DEP discard a batter whose total wait passes P3. If the group chooses option A
   instead (a 15th signal "Batter collected"), the code, `docs/signals.md`, the port tables,
   the Word document and the UPPAAL batter model (`gen.js`) all change.
8. **State machines live on branches** (`state-machines-fin`,
   `requirement-diagrams-BAT`) and are not on master. Merge them.
9. **Signal descriptions**: Band state change carries five sections (S1 to S5);
   the heartbeat is also sent by PLM; events carry an event id. Update the port
   descriptions in the documents (see `docs/signals.md`).
10. **Deployment view** is missing for the report: containers, broker, SQL Server,
    MongoDB, volumes. Draw it once the container granularity is decided.

## Found by switching parts off live (fixed, kept as scenarios)

- With SQL Server off the monitor stalled (it wrote every event to SQL on the same
  worker that processes heartbeats), declared every node silent and halted the
  line. Now the monitor's database writes run in the background, in order and
  retried, and a node is "heard" when its message arrives. Scenario `sql-down`.
- With MongoDB off every document save waited for the driver's 30 s default. Now
  every call has a 3 s limit and the stations carry on. (The `mongo-down` scenario was dropped on the team's decision; Stop on the MongoDB component still shows it live.)
  While MongoDB is off the pan documents are not updated, so a crash in that window
  rebuilds queues from stale documents (not covered).

- A node whose message handler waited on a full internal queue (THM, with its
  batter queue full while the line was halted) also stopped answering MQTT
  keep-alives, dropped off the broker and never reconnected. Message handlers now
  only queue the message and a worker processes them, and the internal queues are
  larger. Found when a broker restart met a halted THM; no scenario reproduces it
  on its own yet (a long halt with BAT producing, then a broker restart).
- "Heard from" for the watchdog must be a heartbeat, not any message: a controller
  that stopped answering (FC28) may still send handoff signals. Scenario
  `faults-maintenance` (FC28) covers it.

- After a BAT crash the flow once took 95 s instead of about 15 s: orders that BAT
  had received but not started were lost with it and PLM waited for them. PLM now
  sends them again (U13); confirmed by a resend event during repeated `crash-bat`
  runs, all about 20 s. No scenario forces that exact timing.

## Failure-case coverage

All of FC1 to FC28 are implemented and run by a scenario (`docs/failure-cases.md`).
Found and fixed while checking against the report: FC20 had no "then Maintenance"
step, the repeated-failure rule for FC24 was random instead of reproducible, and
FC18 (buffer full) was never reported as an event.

Still simpler than the report: FC28 per controller instead of per machine, FC13
detected at once (no T_resp), FC16 in-time release has no scenario of its own.

## Exercises 6 and 7

Drafts written (edit them for the report, they are a starting point):
`docs/communication.md`, `containers.md`, `languages.md`, `databases.md` (exercise 6) and
`docs/experiment.md` with the data in `docs/experiment-results.csv` (exercise 7).

Still open **[person]**:
1. Exercise 6.1: the Papyrus diagram of the message flow with the bus (the text diagram
   in communication.md is a placeholder); check the technology comparison and add
   sources the group has read.
2. Exercise 6.2: "images built and pushed by CI/CD" only happens on main or master;
   it needs the first merge or a manual run on the default branch, and the ghcr
   package settings (visibility).
3. Exercise 6.4: the ER diagram is a Mermaid diagram in docs/databases.md (renders on GitHub); export it as an image for the report (mermaid.live) if needed.
4. Exercise 7: the group decides the metrics and number of runs; the analysis is the
   next exercise. The experiment covers crash and freeze only, on one machine at
   real speed; the availability percentage of THM-REQ-71 is not computed yet.
5. Scalability is asked for in the exercises and is not measured anywhere (one broker,
   five nodes, about 20 pans).

## Fixes that change what the diagrams and documents should say

Rule for the implementation work: every fix that changes behaviour, a signal or a
data shape gets a line here, so the Papyrus models, the Word documents and the
report are updated afterwards. **[person]** = needs Papyrus or Word.

| # | Fix | Update **[person]** |
|---|---|---|
| U1 | FC20 is now "System, then Maintenance": the monitor first tries to correct the speed itself, and when that fails it opens a ticket for `mon-section-N` | MON document and diagrams (none yet); FC20 flow in the report already says "System, then Maintenance" and now matches |
| U2 | FC24 repeated failure: the same quality check failing 3 times in a row calls Maintenance (machine `fin-inspector`), as the general failure-case rule says | FIN document: add a requirement; FIN SM1 (Inspection Flow): add the lockout after repeated rejects and its repair; FIN interface table (Repair confirmed also goes to the inspection) |
| U3 | FC18: the buffer reports "full" the moment the 8th cake is held, stops new pans, and the exit diverts what still comes | THM SM4 T8 already says "report buffer full"; check that THM-REQ-48 and -57 name the event and the buffer-full device state |
| U4 | UC5 alternative: the Weight-Checker is not calibrated, depositing waits for recalibration (guard of THM SM1 T1) | Report use case UC5 and UC6: add the alternative flow; THM SM1: add the "not calibrated" transition or state (today only the guard exists) |
| U5 | BAT reserves ingredients again for a batch that replaces a discarded one (reservation id `<order>-<n>`) | BAT-REQ-03 says "once per order": reword to "per batch"; PLM document: reservations may carry a batch suffix |
| U6 | Signals now carry a message id (handoffs) or an event id (events), are sent with QoS 1 over persistent sessions, and are deduplicated by the receiver; state signals repeat with every heartbeat | All interface tables in BAT, FIN, THM, PLM documents and the signal-port notes in the models; see `docs/signals.md` |
| U7 | Liveness: a node counts as alive only through its birth certificate or heartbeat (not through other signals); the monitor declares it silent after 3 heartbeat intervals; the broker declares a frozen node dead after 1.5 x keep-alive (7.5 s) | THM-REQ-72 (15 s) text can cite both paths; MON document |
| U8 | Monitor database outage: the monitor does not depend on the database for health detection; its writes queue and are retried | MON document (quality attribute scenario: availability when the database fails); report section on availability |
| U9 | Restart recovery: FIN and THM rebuild queues from the stored pan and cake documents; THM resumes cakes on the band and restores the cooling buffer | THM, FIN and BAT state machines (FC27 behaviour, item 6 above); THM deployment table ("saved state") |
| U10 | After depanning a cake has its own document (`CAKE-<batch>-<pan>`), the pan keeps its own | FIN document data description; data model and traceability (batch to pan to cake to pallet) in the report |
| U11 | Component control (`ctl`, demo profile) and the Test scenarios / Components cards | Deployment view: add the `ctl` container and the Docker socket; report: how the demonstrations are run |
| U12 | The "no production without the monitor" rule is implemented through the Sparkplug `STATE` topic of the host | System IBD: add the `STATE` message between MON and every node (see item 1) |
| U13 | PLM sends a production order again when batter preparation has not asked for its ingredients within 40 s (BAT may have restarted and lost it); BAT ignores an order id it already has | PLM state machine (order lifecycle: released, sent again, reserved, held; to be drawn); BAT document: "Production order and recipe targets" is idempotent on the order id; signal table (message id) |
| U15 | Band transport is its own system (BT): sections, FC19 and FC20 moved from MON to BT; THM, FIN and MON take the band state from BT; MON sends BT "Section command" and "Target speed"; machine ids are `bt-section-N` | BT and MON models (ports, see team-review section 6), alignment baseline signal table (Band state change: BT to THM, FIN, MON; two new commands), THM and FIN interface tables (source of Band state change), FC19 and FC20 flows (actors: Band Transport, MON) |
| U16 | The experiment dashboard is a separate container (`ui`), MON has no UI and only a small API (`/api/repair`, `/api/events`, `/api/tickets`, `/api/discards`); THM stops depositing as soon as section 1 is halted; scenarios can be stopped from the dashboard | Deployment view and container table (new `ui` container, MON without port 8080); MON model: the "overseer dashboard" device (see team-review section 2 item 16); THM state machine (depositing waits for the band, S1 halt, admission re-checked after a pan is taken) |
| U17 | Group decisions of 3 Nov: PLM sends a heartbeat like every system; T_hb 5 s for all; 30 min of UC10 = 40 s (factor 45); `mongo-down` dropped; availability counts the controller, not the line | alignment baseline signal table and PLM interface (PLM sends Machine status), the report's time-constraint table, the availability definition in the experiment chapter; optional: a second experiment for THM-REQ-76 and -78 (1 s) |
| U18 | One container per station (14 stations + BT + MON, one image): ten new signals between the stations, band sections owned by stations, upstream halt rule in BT, machine ids and failure-case nodes per station | deployment view (16 containers); optional, because the logical models still hold: name the 10 handoffs inside each system as internal ports (docs/stations.md), BAT Mixing and Handoff (the batch result signal back to dosing), and in the interface tables the station that sends or receives each of the 14 signals |
| U14 | After a PLM restart the order numbers continue after the highest stored order (before: they restarted at PO-0001, BAT ignored the repeated ids and the line stalled; found by run 3 of the availability experiment, kept in `docs/experiment-aborted-run.csv`) | PLM state machine (order lifecycle: the order id is persistent); PLM document: "order ids are unique and never reused"; the experiment report (unexpected behaviour, first experiment run aborted and repeated) |

## Papyrus models of BT and MON (branch MON-BT-Integration) against the code

See docs/team-review.md section 6 for the port table. To do **[person]**:
1. Add ports (or connectors) for the signals the code uses and the models lack: Band
   state change (BT to THM, FIN, MON), events (all to MON), Repair confirmed (MON to
   all), Section command and Target speed (MON to BT).
2. Connect the parts in the main IBD (ASAAT.uml has the BT and MON parts, no
   connectors).
3. MON model: add the commands to BT; BT model: add the band state output.
4. Pan Spacing & Position Tracking: decide whether it belongs in BT (see above).

## Decisions for the group

The open decisions are in [docs/team-review.md](docs/team-review.md) section 1; the ones
already taken are in [docs/decisions.md](docs/decisions.md) section 7; the parameter values
and their reasons are in [docs/assumptions.md](docs/assumptions.md). T_resp is not used (a
broken camera is detected at once in the simulation).

## Verified in Docker (1 Nov 2026, Docker Desktop on Windows 11)

- `docker compose up --build`: all 8 containers become healthy; start order and
  healthchecks work; read-only root with the `/data` volume works.
- SQL Server store: schema, seed, reservations, orders, shipments, event log and
  tickets written and read. A race between PLM and MON creating the schema is fixed
  with an application lock.
- MongoDB store: pan documents and node state written and read.
- Crash of THM (real process exit): death certificate seen by MON after 0.4 s,
  Docker restarted the container, birth seen after 0.8 s, 5 cakes on the band taken
  up again (bounds: 15 s detection, 60 s recovery).
- Frozen THM (`docker pause`): the broker declared it dead after 7.0 s (keep-alive
  5 s), MON saw it return after `unpause`.

- Every other container crashed in turn (PLM, BAT, FIN), MON stopped while cakes are
  discarded (no event lost), the MQTT broker restarted, THM frozen: all scenarios
  in `docs/demo.md` pass against the real containers. They also run in CI.
- Failure cases: all system-resolved cases and all Maintenance cases (error, ticket,
  repair, recovery) pass with `go run ./cmd/scenario faults-system faults-maintenance`.

Still to check **[person]**:

1. Apple silicon: the SQL Server image is x86 and needs Rosetta emulation.
2. Load assumption A1 (about 50 messages per second) against Table II of the report.
3. The GitHub Actions pipeline has only been checked with actionlint and hadolint;
   it has not run on GitHub yet (first push shows it).

## Code gaps

0. **Pan Spacing & Position Tracking** (report, Band Transport) is not in the BT model and
   not implemented. Needs a group decision first (docs/team-review.md section 4, item 0).
   (docs/team-review.md section 4, item 0).

1. **Tests**: unit tests cover only the Sparkplug codec and the signal mapping; the
   behaviour is checked by the scenarios (`docs/demo.md`), not by Go tests.
2. **Deployment**: the pipeline pushes images to ghcr.io but has no deploy job (no
   target machine yet).
3. **THM resume** continues a baking pan from zone 1 (so it bakes longer), loses
   the zone temperature offset, and treats buffered cakes as hot. A cake that was
   being deposited at the moment of the crash is not resumed.
4. **MON policy**: only band faults and unresponsive nodes halt or slow the band.
   The general UC2 error flow (any error may require a halt) needs a policy table
   once the MON document exists.
5. **Duplicate filter** is in memory. After a receiver restart a duplicate is
   caught only by the checks in the receivers (batch id, pan id).
6. **PLM** keeps held orders in memory; after a PLM restart a held order is
   noticed again when BAT repeats its request, not before.
7. The `ctl` container (demo profile only) holds the Docker socket and can control
   every container on the machine; it is not for production.
8. Security is not a stated requirement and is not implemented (anonymous broker,
   no TLS, no dashboard login).
   (a single broker is an accepted risk).
10. Real equipment (PLC, OPC UA, cameras) is out of scope: all machines are
   simulated inside the services.

## Report

1. Write the report sections that use this: the decisions (`docs/decisions.md`),
   the signal table (`docs/signals.md`), the conformance table (section 6 of
   `docs/decisions.md`).
2. Contributions table and the evaluation experiment are empty in the template.
