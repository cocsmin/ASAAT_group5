# For the team to review

Everything the implementation session found odd, left unfixed, decided on its own,
or decided without it being written down elsewhere. Go through it together and
cross out what you accept. Items that need a Papyrus or Word change are also in
[TODO.md](../TODO.md); this file is the discussion list.

All assumptions in one list: [assumptions.md](assumptions.md).

Legend: **[D]** needs a group decision. **[W]** was decided by the implementation
(you may want to veto it). **[B]** known bug or limit, not fixed. **[?]** something
odd that is worth a look.

## 1. Time constraints (decided 3 Nov, the group asked the implementation to decide)

The group let the implementation decide the times and check them for consistency. The
answers are in "Decisions" at the end of this section. Every value that more than one
system depends on now lives in `internal/timing/timing.go`, and
`internal/timing/timing_test.go` (run by CI with `go test ./...`) fails when a relation
between them breaks. The tables below list every value; the ones marked [W] are
assumptions made so that the simulation is watchable, not facts about a real plant. All durations are *simulated* seconds: with `SIM_SPEED=1`
they equal real seconds; the experiment ran at 1.

**From the documents (check that they are right)**

| Value | Source | Used as |
|---|---|---|
| heartbeat T_hb = 5 s | THM proposal; BAT P4 and FIN have no value | every system sends its heartbeat every 5 s [D]: one value for all |
| detect a stopped controller within 15 s (3 x T_hb) | THM-REQ-72 | MON watchdog marks a node silent after 3 x T_hb (15 s) |
| back to normal within 60 s after repair | THM-REQ-73 | measured: about 1 s for a container restart |
| new pans stop within 1 s; reaction to band change within 1 s | THM-REQ-76, -78 | **not measured** |
| discard event within 5 s | THM-REQ-77 | **not measured** (events arrive in well under a second, but nobody timed it) |
| cooling-buffer waiting limit T_wait = 30 min (default) | THM document (cooling buffer requirements) | compressed to **40 s** (see below) |
| cake core temperature 97 C +/- 0.5 C at the tunnel exit | UC9 business rule | used as is |
| cakes packaged only at room temperature | UC10 | T_safe = 30 C [D] |
| 30 min between cooling failure and repair decides refuse or recool | UC10 | compressed to the same 40 s |

**Chosen by the implementation [W]: please confirm or change**

| Where | Value | Meaning |
|---|---|---|
| MQTT | keep-alive 5 s | broker declares a dead node after 7.5 s; THM-REQ-72 allows 15 s |
| PLM | a new order every 22 s, at most 2 not yet picked up | sets the pace of the whole line |
| PLM | warehouse request timeout 2 s, circuit breaker open 15 s | FC2 |
| PLM | Logistics restocks 15 s after the alarm, adds 2 x recipe + 5000 g | FC1 |
| PLM | order sent again after 40 s without an ingredient request | recovery of lost orders |
| BAT | dosing 1.2 s per ingredient, top-up 1 s, 3 failed doses lock the doser (P2 = 3), dose tolerance 2 % (P1) | |
| BAT | mixing 6 s (recipe), human inspection 3 s (simulated, always "good" unless UC4 is injected) | |
| BAT | batter waits at most 25 s (P3) | FC7 |
| BAT | ingredient request repeated after 20 s | recovery |
| THM | deposit 1 s per pan, 3 wrong fills lock the depositor (N_wrong = 3) | FC9, FC10 |
| THM | zone transit 2 s x 3 zones, zone flagged faulty after 4 s out of range (T_adjust) | FC11 |
| THM | exit check 0.5 s, cooling 4 s, buffer capacity 8, buffer wait limit 40 s | FC14 to FC18 |
| THM | depositing jam lasts 30 s (FC7), no-pan wait 4 s (FC8), recalibration 6 s (UC5) | injected faults |
| FIN | depanning, inspection, wrapping, cartoning 1 s each, washing 2 s + drying 1 s | |
| FIN | holding area 6 cakes, 3 rejects of the same check call Maintenance, pallet = 6 cakes, 20 pans | |
| MON | node marked "dead" and escalated after 45 s without restart; FC20 wrong speed corrected after 10 s; sections restart 1.5 s apart | |
| all | state snapshot every 3 s; event outbox retried every second; database calls time out after 3 s | |

**Decisions (3 Nov)**
1. **Seconds, not real plant time.** The simulation runs in seconds; Table II of the
   report gives no real numbers (belt speed, baking time, throughput), so none are
   invented. `SIM_SPEED` speeds the whole production up or down.
2. **UC10's 30 minutes = 40 s, a fixed documented factor of 45** (one simulated second
   stands for 45 real seconds; `timing.Compression`, checked by the test). Not a
   parameter: a parameter would let the two UC10 rules (buffer wait, refuse or recool)
   disagree. Only these two UC10 values are compressed; the other production times are
   short because a watchable line needs them short, not because of this factor.
3. **T_hb = 5 s for every system**, including PLM (see decision 4 below and section 2).
   No per-system values: the monitor would need a table and a mistake would show up as
   a false "silent" alarm.
4. **Both detections stay.** The requirement is THM-REQ-72 (15 s = 3 x T_hb, the MON
   watchdog). The MQTT keep-alive (5 s, dead after 7.5 s) is faster and sends the
   death certificate; it is an extra, not the requirement.
5. **THM-REQ-76 and -78 (1 s) are not measured now.** They need a second experiment
   with a timer inside THM; listed as optional in TODO.
6. **Availability (THM-REQ-71, 99.5 %) counts the controller, not the line.** Downtime
   is the time the controller does not answer (measured about 1 s per crash). The time
   until the line produces again (10 to 60 s, the line refilling) is reported next to
   it as "production recovery", not as downtime. Assumed fault rate: one fault per
   hour, which gives about 99.97 % for a 1 s outage. This is an assumption to name in
   the report.
7. **Dose tolerance 2 %, fill 450 +/- 5 g, T_safe 30 C, pallet of 6 stay as assumed
   values** until someone has real ones. Dose tolerance, fill target and tolerance are in the recipe
   seed (SQL), the pallet size and pan count are environment variables, T_safe is a
   constant in `internal/thm/thm.go`.

**Consistency check (done 3 Nov)**
- Supervision timers (heartbeat, keep-alive, timeouts, 45 s escalation) are wall-clock;
  production times and every handoff timer (batter wait, ingredient retry, order
  resend, buffer wait, breaker, restock) are simulated time (`sim.Scale`). That split
  is deliberate and now written down in `internal/timing`.
- Found and fixed: the same values were typed in several files (5 s heartbeat in two,
  3 s database timeout in three, 20 s and 40 s in the retry paths). They are now one
  constant each.
- The test checks: 3 x T_hb <= 15 s; the broker's 7.5 s <= 15 s; events and state are
  saved at least as often as the heartbeat; the 45 s escalation is after detection and
  before THM-REQ-73's 60 s; PLM resends an order only after BAT had two ingredient
  retries (40 >= 2 x 20); the warehouse breaker (2 s) trips before the 3 s database
  timeout; the compression factor gives exactly 30 minutes.
- Not covered by the test (inside one system only): FIN, THM and BT step times. Only
  spot-checked by reading the code, not proven: no step time was found that is longer
  than the timer of the system that waits for it.

## 2. Decisions taken by the implementation, not (fully) in the docs [W]

1. **PLM creates the batch id** with the order (`B0001`); a restarted batch gets a
   suffix (`B0001-2`) and its ingredient reservation `PO-0001-2`. Open item of the
   alignment baseline; in `decisions.md`, but the suffix rule is not.
2. **Band state change is sent by BT** to THM, FIN and MON, carries all five sections
   (and the detail "fault" for MON), and is sent as ordinary data repeated every
   heartbeat (not as a command, not retained). MON commands BT with "Section command"
   and "Target speed".
3. **PLM sends a heartbeat** like every other system. **Decided by the group (3 Nov):** yes. The alignment baseline (PLM sends none) has to be corrected [W: Papyrus]. Item 3.5 below is closed.
4. **Every handoff signal carries a message id** and events an event id (not in any
   port description); receivers drop duplicates. Both are in `signals.md`.
5. **Two extra messages** outside the 14 signals: `Fault injection` (demo) and `Node
   Control` (Sparkplug rebirth).
6. **Sparkplug deviations**: nodes read each other's NDATA directly; QoS 1 and
   persistent sessions for handoffs; state repeated with the heartbeat instead of
   MQTT retained messages. In `decisions.md` section 1.
7. **"No production without the monitor" is enforced** (THM admits no pans, BAT starts
   no batch, PLM releases no order while MON is offline). Not obvious from the use
   case text, and it stops the line whenever MON restarts: a MON restart pauses
   production. Confirm that this is what UC2 means.
8. **Inspector is simulated** (3 s, "good"). The real human gate is a button nobody
   built; do we want one on the dashboard?
9. **A cake gets its own document** (`CAKE-<batch>-<pan>`) after depanning.
10. **One order, 6 pans, one recipe ("Vanilla sponge") with seed ingredients and stock
    of 9000 g each**; trucks are assigned round-robin among three. All invented.
11. **Dashboard buttons play Maintenance**: *Mark repaired* confirms any repair; there
    is no login and no role. The `ctl` container (demo profile) can control every
    Docker container on the machine.
12. **Hand-in workflow**: `PROJECT_NAME=ASAAT-cake-factory`, `GROUP_NUMBER=Group05` are
    a guess from the repository name. Please correct.
13. **CI**: the root `Dockerfile` (Node template) was deleted, the deploy job and all
    `continue-on-error` were removed, images are pushed per system to
    `ghcr.io/<owner>/cake-factory-<system>` only from main or master.
14. **The exercise documents** (`communication.md`, `containers.md`, `languages.md`,
    `databases.md`, `experiment.md`) are drafts written by the assistant: check every
    claim, add your own sources, and make them fit the report template.
16. **The dashboard is its own container (`ui`), not part of MON.** MON is the real
    system (health, event log, tickets, commands to BT) and has no user interface; the
    dashboard exists only for the experiments and for showing the demo. It reads the
    Sparkplug messages, SQL Server and MongoDB itself and publishes the fault injections,
    so it still shows the outage when MON is down. It calls MON only for "repair
    confirmed" (the Maintenance action). The MON model's "overseer dashboard" device is
    therefore **not** the page you see: [D] decide whether the real plant has an overseer
    UI inside MON (then the model stays) or the dashboard is demo-only (then the model
    needs a note).
15. **Dev mode** (`go run ./cmd/dev`) keeps SQL data in memory and pans as JSON files,
    wipes `data/` at each start; it exists only so the factory runs without Docker.

## 3. Decisions waiting for the group [D]

1. **Container granularity**: one container per system (now) or split THM baking
   zone control (THM-REQ-74) and BAT dosing/mixing (BAT-NFR-06). Recommendation: those
   two only. A separate health container is not needed.
2. **The justification of the decisions already taken** (Sparkplug B on MQTT, SQL
   Server for the main system, MongoDB for the pans, Go, Docker) has to be written in
   the report; the drafts in `communication.md`, `databases.md`, `languages.md` and
   `containers.md` are a starting point.
3. **A "batter collected" acknowledgement** between THM and BAT (BAT Mixing and Handoff
   has it, the 14 signals do not).
4. **BAT frame port "Fault event"**: internal or a 15th signal.
5. ~~Whether PLM sends a heartbeat~~ decided 3 Nov: yes (see 2.3).
6. **Which failure cases are measured** in the experiment beyond crash and freeze.
7. **Scalability**: the exercises ask for it; nothing is built or measured. Do we add
   a small test (more pans, more messages) or only discuss it?
8. **Security** is not a stated quality attribute; broker is anonymous, no TLS, SQL
   password in the compose file with a demo default.
9. **Merge**: the branch `implementation/go-sparkplug-factory` is pushed, not merged,
   no pull request. The CI pipeline has never run on GitHub.

10. **Role of MongoDB.** The group decided (3 Nov) to drop the `mongo-down` scenario ("not
    relevant for our experiment"). It is dropped. In the code it also holds the traveling pan
    documents (traceability) and the saved node state that THM, BAT and FIN reload after a
    restart. [D] Confirm that this is experiment data only. If yes, say so in the
    databases document and the report; if no, bring `mongo-down` back (one line in
    `internal/scenario/scenario.go`).

## 4. Known bugs and limits, not fixed [B]

0. **Band Transport is now a system of its own** (branch MON-BT-Integration, models
   `BT.uml` and `MON.uml`) and a sixth service in the code. Still open: **Pan Spacing &
   Position Tracking**, which the report lists under Band Transport, is **not in the BT
   model** and is not implemented (pans have a station, no position or spacing rule).
   [D] Was it dropped on purpose? If not, it has to be added to the model and the code.
1. **Crash while MongoDB is down**: the pan documents are not updated during the
   outage, so a restart in that window rebuilds queues from stale documents.
2. **THM restart**: a resumed pan bakes from zone 1 again; a pan being filled at the
   moment of the crash is not resumed; buffered cakes come back as "hot" (60 C).
3. **BAT restart**: a batch in the middle of dosing is lost (allowed by BAT-NFR-02).
4. **Duplicate filter is in memory**: after a receiver restart a duplicate is caught
   only by the receivers' own checks (batch id, pan id, order id).
5. **PLM keeps held orders in memory**; after a restart a held order is noticed again
   when BAT repeats its request (up to 20 s later).
6. **MON event list** in memory shows the latest 400 (the dashboard 80); SQL has all.
   Ticket ids on the dashboard come from memory and are mapped to SQL rows later.
7. **FC28 per controller**, not per machine; **FC13** detected at once (no T_resp);
   **FC16** in-time release implemented but no scenario waits for it; **FC20**
   correction is a 10 s timer, there is no speed sensor.
8. **Scenarios from the dashboard**: `freeze-thm`, `band-halt`, `sql-down` need Docker; there is no `mongo-down` scenario any more (MongoDB is treated as experiment data, see section 3 [D]; the code still survives MongoDB being off, it was measured once, see docs/experiment.md) (not in dev mode). The *Stop* button ends a run and restarts every part the scenario left stopped or frozen; a scenario that was stopped in the middle is not counted as passed or failed.
9. **Experiment data** is from one machine at real speed, 10 runs per scenario, only
    crash and freeze; the aborted first run is kept on purpose.
10. **Code**: unit tests cover only the Sparkplug codec and the signal mapping; the
    behaviour is checked by scenarios, which need a running stack.

## 5. Odd things worth a look [?]

1. **Outage vs recovery**: a system is back in under a second, but production takes 10
   to 60 s to resume. Is the requirement about the controller or about the line?
2. **Crash detected in 0.3 s, freeze in 5 to 8 s** (and the frozen case is slower than
   the report assumed). Which one should THM-REQ-72 really mean?
3. **Same fault, different timing**: outage depends on where in the 22 s order cycle
   the fault lands, which is why p95 is much higher than the median.
4. **Line endings**: the repository has no `.gitattributes`; Git warns "LF will be
   replaced by CRLF" on Windows. Harmless, but files may show as fully changed for
   teammates on other systems.
5. **Machine setup of whoever ran this**: Go and Docker Desktop were installed during
   the session; the PC's lid and sleep settings were changed by hand for the
   experiment. Nothing in the repository depends on that.
6. **`docs/decisions.md` section 6 (conformance table)** was written midway and
   edited afterwards; re-read it against the code before using it in the report.
7. **`go.mod` says Go 1.25** though it was created with a newer toolchain; the Docker
   build with `golang:1.25` works (verified), other versions are not tested.
8. **Several scenario outputs contain simulated numbers** (pans, cakes shipped) that
   are not factory-realistic; do not quote them as throughput.

## 6. Papyrus models of BT and MON against the implementation

Read from `Model/PapyrusProject/BT.uml`, `MON.uml` and `ASAAT.uml` (branch MON-BT-Integration).
Implemented as the models describe: BT-AF-01 Section Control, BT-AF-02 Speed Monitoring,
BT-AF-03 Transport Fault Handling and the devices conveyor controller, drive, speed
sensor and section status sensor (as Sparkplug devices); MON-AF-01 Health Monitor,
MON-AF-02 Event / Traceability Logger, MON-AF-03 Maintenance Manager, MON-AF-07
Container Supervision, and the devices message bus (Mosquitto), log database (SQL
Server) and overseer dashboard.

| Papyrus port | Implemented as | Difference |
|---|---|---|
| BT in: targetSpeed | NCMD "Target speed" from MON | BT-only value; nobody sends it yet except by hand |
| BT in: sectionCommand | NCMD "Section command" from MON | new signal name (sentence case), group to confirm |
| BT out: transportFault | event signal, severity error, machine id `bt-section-N` | not a signal of its own |
| BT out: controllerStatus | heartbeat | same thing, name differs |
| MON in: sensorData, containerStatus | heartbeat, birth and death certificates | names differ |
| MON out: healthStatus, healthAlert, maintenanceStatus | dashboard (alerts, tickets) | internal to MON, no outgoing signal |

**Signals the code uses that the models have no port for [D]:** "Band state change"
(BT out; THM, FIN and MON in), "Log, warning, error and discard events" (every system
out, MON in), "Repair confirmed" (MON out; every system in), "Section command" and
"Target speed" (see above). The main model (ASAAT.uml) has the parts but **no
connectors yet**, so the system IBD cannot show these flows.

## 7. Where everything else is

[README.md](../README.md) (run it), [TODO.md](../TODO.md) (diagrams, code gaps, the
U1 to U14 table of document updates), [failure-cases.md](failure-cases.md),
[decisions.md](decisions.md), [signals.md](signals.md), [demo.md](demo.md),
[experiment.md](experiment.md).
