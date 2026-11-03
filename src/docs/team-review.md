# For the team to review

What is still open: decisions the group has to take, choices the implementation made on
its own that you may want to veto, known bugs and limits, and odd things worth a look.
What the group has already decided is in [decisions.md](decisions.md) section 7; the
values the simulation assumes are in [assumptions.md](assumptions.md). Items that need a
Papyrus or Word change are also in [TODO.md](../TODO.md).

Legend: **[D]** needs a group decision. **[W]** was decided by the implementation (you
may want to veto it). **[B]** known bug or limit, not fixed. **[?]** something odd that
is worth a look.

## 1. Decisions waiting for the group [D]

1. **The assumptions marked "Confirm" in [assumptions.md](assumptions.md)**: the
   demo-only dashboard (S2), the rule "no production without the monitor" (S4, a MON
   restart pauses the line), the Sparkplug deviations (S8), the 25 s batter wait (T7), and
   the fault rate behind the availability figure (E2).
2. **A "batter collected" acknowledgement** from depositing to batter preparation. The BAT
   state machine Mixing and Handoff waits for it; none of the 14 signals carries it.
   Option A: a 15th signal, so BAT can free its station and send again if it never comes.
   Option B: remove the waiting step from the BAT diagram. Recommendation: B unless BAT must
   resend after a depositing crash.
3. **BAT frame port "Fault event"**: internal or a signal. In the code a BAT fault already
   leaves as signal 12 (log, warning, error and discard events), so a separate signal would
   duplicate it. Recommendation: internal, shown as an output of signal 12.
4. **Pan Spacing & Position Tracking** (report, Band Transport) is not in the BT model and
   not implemented (a pan has a station, no position or spacing rule). Dropped on purpose?
   If not, it has to be added to the model and the code.
5. **The overseer dashboard in the MON model.** The page you see is demo-only (its own
   container). Does the real plant have an overseer UI inside MON (the model stays) or not
   (the model needs a note)?
6. **The justification of the decisions already taken** (Sparkplug B on MQTT, SQL Server,
   MongoDB, Go, Docker, one container per station) has to be written in the report; the
   drafts in `communication.md`, `databases.md`, `languages.md` and `containers.md` are a
   starting point.
7. **Which failure cases are measured** in the experiment beyond crash and freeze.
8. **Scalability**: the exercises ask for it; nothing is built or measured beyond 60 pans
   and 15 nodes. A small test (more pans, more messages) or only a discussion?
9. **Security** is not a stated quality attribute: the broker is anonymous, no TLS, the SQL
   password is in the compose file with a demo default. Name it as out of scope in the
   report?
10. **Merge**: the branch `implementation` is pushed, not merged, no pull request yet. The
    CI pipeline has never run on GitHub (to be checked once the pull request exists).

## 2. Decided by the implementation, not (fully) in the docs [W]

1. **PLM creates the batch id** with the order (`B0001`); a restarted batch gets a suffix
   (`B0001-2`) and its ingredient reservation `PO-0001-2`. Open item of the alignment
   baseline; in `decisions.md`, but the suffix rule is not.
2. **Band state change is sent by BT** to the stations of THM and FIN and to MON, carries
   all five sections (and the detail "fault" for MON), and is sent as ordinary data repeated
   every heartbeat (not as a command, not retained). MON commands BT with "Section command"
   and "Target speed".
3. **A stopped section stops everything upstream of it**, also when MON halts a station
   that is down: Band Transport halts that station's section and every section before it.
4. **Every handoff signal carries a message id** and events an event id (not in any port
   description); receivers drop duplicates. Both are in `signals.md`.
5. **Extra messages** outside the 14 signals: the ten handoffs between stations, `Section
   command`, `Target speed`, `Fault injection` (demo), `Node Control` (Sparkplug rebirth).
6. **Sparkplug deviations**: nodes read each other's NDATA directly; QoS 1 and persistent
   sessions for handoffs; state repeated with the heartbeat instead of MQTT retained
   messages. In `decisions.md` section 1.
7. **The pan or cake document is the truth, the message is the trigger**: a station adopts
   what the station before it handed over by scanning the documents every 5 s
   ([stations.md](stations.md)).
8. **Inspector is simulated** (3 s, "good"). The real human gate is a button nobody built;
   do we want one on the dashboard?
9. **A cake gets its own document** (`CAKE-<batch>-<pan>`) after depanning.
10. **Dashboard buttons play Maintenance**: *Mark repaired* confirms any repair; there is no
    login and no role. The `ctl` container (demo profile) can control every Docker
    container on the machine.
11. **Hand-in workflow**: `PROJECT_NAME=ASAAT-cake-factory`, `GROUP_NUMBER=Group05` are a
    guess from the repository name. Please correct.
12. **CI**: the root `Dockerfile` (Node template) was deleted, the deploy job and all
    `continue-on-error` were removed, images are pushed per image (stations, BT, MON, ui) to
    `ghcr.io/<owner>/cake-factory-<image>` only from main or master.
13. **The exercise documents** (`communication.md`, `containers.md`, `languages.md`,
    `databases.md`, `experiment.md`) are drafts written by the assistant: check every
    claim, add your own sources, and make them fit the report template.

## 3. Known bugs and limits, not fixed [B]

1. **Crash while MongoDB is down**: the pan documents are not updated during the outage, so
   a restart in that window rebuilds from stale documents.
2. **THM restarts**: a pan inside the tunnel bakes from zone 1 again after the baking
   station restarts; a pan being filled at the moment of the depositing crash is not
   resumed (the batches waiting for depositing are saved and do come back); buffered cakes come
   back as "hot" (60 C).
3. **A BAT batch lives in three containers** (dosing, mixing, inspection). If mixing or
   inspection restarts, the batch it held is gone; dosing, which waits for the result,
   discards it after 120 s (`batchLostAfter`, an assumption; food safety: nothing of unknown
   age is used) and starts a new batch. The ingredients are already used up. So a restart
   costs about two minutes of batter. Before the split the same restart lost the batch at once.
4. **Duplicate filter is in memory**: after a receiver restart a duplicate is caught only by
   the receivers' own checks (batch id, pan id, order id).
5. **PLM keeps held orders in memory**; after a restart a held order is noticed again when
   BAT repeats its request (up to 20 s later).
6. **MON event list** in memory shows the latest 400 (the dashboard 80); SQL has all. Ticket
   ids on the dashboard come from memory and are mapped to SQL rows later.
7. **FC28 per controller**, not per machine; **FC13** detected at once (no T_resp); **FC16**
   in-time release implemented but no scenario waits for it; **FC20** correction is a 10 s
   timer, there is no speed sensor.
8. **Backpressure between the FIN stations is gone**: quality inspection no longer waits when
   the holding area in front of wrapping is full; the holding area grows and wrapping asks
   depositing to stop.
9. **Experiment data** is from one machine, simulated station times, one fault at a time (plus
   two scenarios with several stations); see experiment.md for what the numbers can and cannot
   say.
10. **Tests**: unit tests cover the codec, signals, timing, model, station helpers, node
    dispatch, stores, the dashboard observer, band logic, the adoption of handed-over pans, the
    depositing queue, the monitor's birth handling and the wiring of the model, the
    station program and the compose file. They run in CI. The main loop of every station is
    not unit-tested (it needs a broker or a long simulation); the scenarios cover it.

## 4. Odd things worth a look [?]

1. **Outage vs recovery**: a station is back in under a second, but production takes 10 to
   60 s to resume. Is the requirement about the controller or about the line? (Decided for
   the report: the controller; this item stays as a question for the discussion.)
2. **Crash detected in 0.3 s, freeze in 5 to 8 s** (the frozen case is slower than the
   report assumed). Which one should THM-REQ-72 really mean?
3. **Same fault, different timing**: the outage depends on where in the 22 s order cycle the
   fault lands, which is why p95 is much higher than the median.
4. **Line endings**: the repository has no `.gitattributes`; Git warns "LF will be replaced
   by CRLF" on Windows. Harmless, but files may show as fully changed for teammates on other
   systems.
5. **`docs/decisions.md` section 6 (conformance table)** was written midway and edited
   afterwards; re-read it against the code before using it in the report.
6. **`go.mod` says Go 1.25**; the Docker build with `golang:1.25` works (verified), other
   versions are not tested.
7. **Several scenario outputs contain simulated numbers** (pans, cakes shipped) that are not
   factory-realistic; do not quote them as throughput.

## 5. Papyrus models of BT and MON against the implementation

Read from `Model/PapyrusProject/BT.uml`, `MON.uml` and `ASAAT.uml` (branch MON-BT-Integration).
Implemented as the models describe: BT-AF-01 Section Control, BT-AF-02 Speed Monitoring,
BT-AF-03 Transport Fault Handling and the devices conveyor controller, drive, speed sensor
and section status sensor (as Sparkplug devices); MON-AF-01 Health Monitor, MON-AF-02 Event /
Traceability Logger, MON-AF-03 Maintenance Manager, MON-AF-07 Container Supervision, and the
devices message bus (Mosquitto) and log database (SQL Server).

| Papyrus port | Implemented as | Difference |
|---|---|---|
| BT in: targetSpeed | NCMD "Target speed" from MON | BT-only value; nobody sends it yet except by hand |
| BT in: sectionCommand | NCMD "Section command" from MON | new signal name (sentence case), group to confirm |
| BT out: transportFault | event signal, severity error, machine id `bt-section-N` | not a signal of its own |
| BT out: controllerStatus | heartbeat | same thing, name differs |
| MON in: sensorData, containerStatus | heartbeat, birth and death certificates | names differ |
| MON out: healthStatus, healthAlert, maintenanceStatus | the tickets and alerts the dashboard reads | internal to MON, no outgoing signal |

**Signals the code uses that the models have no port for [D]:** "Band state change" (BT out;
THM, FIN and MON in), "Log, warning, error and discard events" (every system out, MON in),
"Repair confirmed" (MON out; every system in), "Section command" and "Target speed" (see
above). The main model (ASAAT.uml) has the parts but **no connectors yet**, so the system IBD
cannot show these flows.

## 6. Where everything else is

[README.md](../README.md) (run it), [TODO.md](../TODO.md) (diagrams, code gaps, the U1 to U18
table of document updates), [failure-cases.md](failure-cases.md), [decisions.md](decisions.md),
[assumptions.md](assumptions.md), [stations.md](stations.md), [signals.md](signals.md),
[demo.md](demo.md), [experiment.md](experiment.md).
