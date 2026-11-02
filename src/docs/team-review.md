# For the team to review

Everything the implementation session found odd, left unfixed, decided on its own,
or decided without it being written down elsewhere. Go through it together and
cross out what you accept. Items that need a Papyrus or Word change are also in
[TODO.md](../TODO.md); this file is the discussion list.

Legend: **[D]** needs a group decision. **[W]** was decided by the implementation
(you may want to veto it). **[B]** known bug or limit, not fixed. **[?]** something
odd that is worth a look.

## 1. Time constraints (still to be debated)

None of the values below was agreed by the group except those in the first block,
which come from the documents. Everything else is a placeholder chosen so that the
simulation is watchable. All durations are *simulated* seconds: with `SIM_SPEED=1`
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

**Questions for the group [D]**
1. Are the real timings of the line known? Table II of the report gives no numbers
   for belt speed, baking time or throughput. The simulation uses seconds; a real
   tunnel bakes for tens of minutes. Do we keep seconds and say so, or scale?
2. How to present 30 minutes (UC10) that is 40 s here: a documented factor (1 min =
   1.33 s) or a configurable parameter?
3. T_hb: 5 s everywhere, or shorter for the 24/7-critical THM and longer for BAT and
   PLM (which are not 24/7-critical)?
4. Is 7.5 s detection from the keep-alive enough, or should the heartbeat alone
   (15 s) be the requirement, since both exist now?
5. THM-REQ-76 and -78 (1 s): do we want them measured in a second experiment?
6. 99.5 % availability (THM-REQ-71): which fault rate do we assume? The experiment
   shows detection and recovery are about 1 s; the production outage (10 to 60 s) is
   dominated by the line refilling, so the answer depends on what counts as downtime:
   the controller, or the line producing.
7. Dose tolerance 2 %, fill 450 +/- 5 g, T_safe 30 C, pallet of 6: real values?

## 2. Decisions taken by the implementation, not (fully) in the docs [W]

1. **PLM creates the batch id** with the order (`B0001`); a restarted batch gets a
   suffix (`B0001-2`) and its ingredient reservation `PO-0001-2`. Open item of the
   alignment baseline; in `decisions.md`, but the suffix rule is not.
2. **Band state change goes to THM and FIN**, carries all five sections, and is sent
   as ordinary data repeated every heartbeat (not as a command, not retained).
3. **PLM sends a heartbeat** although the alignment baseline says it sends none.
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
15. **Dev mode** (`go run ./cmd/dev`) keeps SQL data in memory and pans as JSON files,
    wipes `data/` at each start; it exists only so the factory runs without Docker.

## 3. Decisions waiting for the group [D]

1. **Container granularity**: one container per system (now) or split THM baking
   zone control (THM-REQ-74) and BAT dosing/mixing (BAT-NFR-06). Recommendation: those
   two only. The lecture draft's separate health container is not needed.
2. **Keep Sparkplug B/protobuf and SQL Server + MongoDB** even though the lecture draft
   argued for MQTT 5 with JSON and SQLite + PostgreSQL. The group chose the first;
   `decisions.md` says so, but the report must carry the justification.
3. **A "batter collected" acknowledgement** between THM and BAT (BAT Mixing and Handoff
   has it, the 14 signals do not).
4. **BAT frame port "Fault event"**: internal or a 15th signal.
5. **Whether PLM sends a heartbeat** (see 2.3).
6. **Which failure cases are measured** in the experiment beyond crash and freeze.
7. **Scalability**: the exercises ask for it; nothing is built or measured. Do we add
   a small test (more pans, more messages) or only discuss it?
8. **Security** is not a stated quality attribute; broker is anonymous, no TLS, SQL
   password in the compose file with a demo default.
9. **Merge**: the branch `implementation/go-sparkplug-factory` is pushed, not merged,
   no pull request. The CI pipeline has never run on GitHub.

## 4. Known bugs and limits, not fixed [B]

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
8. **Scenarios from the dashboard**: `mon-down` can only run in a terminal (it would
   cut the page), and `freeze-thm`, `sql-down`, `mongo-down` need Docker (not in dev mode).
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
4. **The lecture draft** (on `requirement-diagrams`) proposes things we did not do:
   JSON message schemas in `schemas/`, SQLite per service, PostgreSQL, a hybrid
   container split, and states "MON not delivered". Decide whether the report
   mentions the alternatives.
5. **Line endings**: the repository has no `.gitattributes`; Git warns "LF will be
   replaced by CRLF" on Windows. Harmless, but files may show as fully changed for
   teammates on other systems.
6. **Machine setup of whoever ran this**: Go and Docker Desktop were installed during
   the session; the PC's lid and sleep settings were changed by hand for the
   experiment. Nothing in the repository depends on that.
7. **`docs/decisions.md` section 6 (conformance table)** was written midway and
   edited afterwards; re-read it against the code before using it in the report.
8. **`go.mod` says Go 1.25** though it was created with a newer toolchain; the Docker
   build with `golang:1.25` works (verified), other versions are not tested.
9. **Several scenario outputs contain simulated numbers** (pans, cakes shipped) that
   are not factory-realistic; do not quote them as throughput.

## 6. Where everything else is

[README.md](../README.md) (run it), [TODO.md](../TODO.md) (diagrams, code gaps, the
U1 to U14 table of document updates), [failure-cases.md](failure-cases.md),
[decisions.md](decisions.md), [signals.md](signals.md), [demo.md](demo.md),
[experiment.md](experiment.md).
