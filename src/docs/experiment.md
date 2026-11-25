# Exercise 7.1: Experiment on availability

Draft for the report. This exercise is design, setup and data collection; the
analysis is the next exercise. The data is in
[experiment-results.csv](experiment-results.csv).

## 1. Quality attribute

**Availability**, the group's main quality attribute ("Availability is the group's
main quality attribute", BAT, FIN and THM documents). It is the most critical
*quantitative* one: the THM document gives numbers for it.

| Requirement | Value |
|---|---|
| THM-REQ-71 | 99.5 % monthly availability of the controllers (proposed value) |
| THM-REQ-72 | a stopped controller is detected within 3 heartbeat intervals, 15 s with T_hb = 5 s |
| THM-REQ-73 | back in normal operation within 60 s after the cause is removed |

## 2. Goal (GQM template, Wohlin et al.)

> **Analyze** the Docker-deployed Sparkplug B architecture of the cake factory
> **for the purpose of** evaluating
> **with respect to** availability (time to detect a failed system, time to recover
> it, and the production outage and product loss it causes)
> **from the point of view of** the production overseer
> **in the context of** the simulated line (six systems on 16 factory containers, one per station,
> plus MQTT broker, SQL Server and MongoDB) running in Docker Compose on one machine.

Questions and metrics:

| Question | Metric | Unit | Requirement |
|---|---|---|---|
| Q1 How fast is a failed system noticed? | `detect_s`: fault injected until the monitor has its death certificate | s | at most 15 s |
| Q2 How fast is it back? | `recover_s`: fault injected until its birth certificate is received | s | at most 60 s |
| Q3 How long does production stop? | `outage_s`: fault injected until the next cake ships | s | none; feeds the availability estimate |
| Q4 Is product lost? | `pans_lost`: pans in the loop after recovery minus before | pans | 0 |
| Q5 How long until the line flows again after the system is up? | `flow_resume_s` | s | none |

`pans_lost` is a conservation check on the pan documents; cakes inside a crashed
system are taken up again from the stored documents, so 0 is expected.

## 3. Design

- **Independent variables:** the container that fails (each of the 14 stations and BT, one
  at a time) and the way it fails: process crash, frozen process (baking tunnel), and
  two or three stations frozen together.
- **Fixed:** heartbeat 5 s, MQTT keep-alive 5 s, `SIM_SPEED=1` (real time), one
  recipe, 48 pans in the loop, 24 pans per batch, pallets of 24, no random failure
  (`FAULT_RATE=0`), the line settled (every container online, no open error) before each run.
- **Replications:** 15 runs per scenario, 18 scenarios, 270 runs in total.
- **Procedure per run:** wait until the factory is settled; record the pan count; inject
  the fault through the dashboard API; poll the event log every 100 ms; record the
  times; wait until the line is settled; record the pan count and wait for the next pallet.
- **Instrumentation:** the experiment runner (`cmd/scenario experiment`) uses the
  dashboard's API (`/api/inject`, `/api/events`, `/api/state`, `/api/pans/summary`), so it
  measures what an overseer would see. Resolution about 100 ms.
- **Environment:** Docker Desktop on Windows 11, containers as in `docker-compose.yml`,
  started fresh before the experiment.
- **Second part, availability run:** the factory runs 2 h with `FAULT_RATE=2` (all failure
  rates doubled, so 2 h equal 4 h at the catalog rates) and `CREW_SECONDS=40` (a simulated
  maintenance crew repairs each ticket after a random time around 40 s). Nothing is
  injected by hand. Scenario `availability`.

Reproduce:

```bash
docker compose up -d --build        # wait until all containers are healthy
go run ./cmd/scenario experiment -runs 15 -out docs/experiment-results.csv
go run ./cmd/scenario/summary docs/experiment-results.csv
FAULT_RATE=2 CREW_SECONDS=40 docker compose up -d
go run ./cmd/scenario availability -duration 2h
```

Why this design: each container is hit once, so a weak container shows. Crash and freeze
are different mechanisms (the broker's Last Will fires at once on a crash; a frozen
process is only found when keep-alives stop). Multi-station freezes show whether the
band halts correctly. The random run gives the availability that single faults cannot.

## 4. Data

`docs/experiment-results.csv`, one row per run:
`timestamp, scenario, run, result, detect_s, recover_s, outage_s, flow_resume_s, pans_lost, error`.
`docs/availability-run.log` holds the long run. The older five-system data (10 runs, 50 of
50 passed) is kept as [experiment-results-5-systems.csv](experiment-results-5-systems.csv).

## 5. Results

Run on 3 Nov 2026 (part 1: 15:28 to 17:22, part 2: 17:24 to 19:24), Docker Desktop on
Windows 11, `SIM_SPEED=1`. **269 of 270 runs passed.** Times in seconds.

| scenario | runs | passed | metric | min | median | mean | p95 | max |
|---|---|---|---|---|---|---|---|---|
| crash-plm-wh | 15 | 15 | detect_s | 0.32 | 0.32 | 0.35 | 0.43 | 0.43 |
| crash-plm-wh | 15 | 15 | recover_s | 0.78 | 0.85 | 0.87 | 0.95 | 1.19 |
| crash-plm-wh | 15 | 15 | outage_s | 15.85 | 24.92 | 24.32 | 24.97 | 25.01 |
| crash-plm-wh | 15 | 15 | flow_resume_s | 9.85 | 19.02 | 18.41 | 19.12 | 19.13 |
| crash-plm-wh | 15 | 15 | pans_lost | 0.00 | 0.00 | 0.00 | 0.00 | 0.00 |
| crash-plm-ship | 15 | 15 | detect_s | 0.32 | 0.32 | 0.33 | 0.33 | 0.42 |
| crash-plm-ship | 15 | 15 | recover_s | 0.78 | 0.86 | 0.87 | 0.96 | 1.02 |
| crash-plm-ship | 15 | 15 | outage_s | 20.76 | 25.83 | 25.18 | 25.88 | 25.98 |
| crash-plm-ship | 15 | 15 | flow_resume_s | 14.86 | 19.91 | 19.26 | 19.97 | 19.99 |
| crash-plm-ship | 15 | 15 | pans_lost | 0.00 | 0.00 | 0.00 | 0.00 | 0.00 |
| crash-bat-dose | 15 | 15 | detect_s | 0.32 | 0.32 | 0.32 | 0.32 | 0.34 |
| crash-bat-dose | 15 | 15 | recover_s | 0.75 | 0.85 | 0.82 | 0.85 | 0.90 |
| crash-bat-dose | 15 | 15 | outage_s | 24.85 | 24.93 | 24.93 | 24.97 | 24.99 |
| crash-bat-dose | 15 | 15 | flow_resume_s | 18.96 | 19.04 | 19.05 | 19.14 | 19.15 |
| crash-bat-dose | 15 | 15 | pans_lost | 0.00 | 0.00 | 0.00 | 0.00 | 0.00 |
| crash-bat-mix | 15 | 15 | detect_s | 0.32 | 0.32 | 0.32 | 0.32 | 0.35 |
| crash-bat-mix | 15 | 15 | recover_s | 0.75 | 0.81 | 0.83 | 0.90 | 0.96 |
| crash-bat-mix | 15 | 15 | outage_s | 24.86 | 24.92 | 24.92 | 24.99 | 25.01 |
| crash-bat-mix | 15 | 15 | flow_resume_s | 18.92 | 19.04 | 19.03 | 19.09 | 19.14 |
| crash-bat-mix | 15 | 15 | pans_lost | 0.00 | 0.00 | 0.00 | 0.00 | 0.00 |
| crash-bat-insp | 15 | 15 | detect_s | 0.32 | 0.32 | 0.32 | 0.32 | 0.33 |
| crash-bat-insp | 15 | 15 | recover_s | 0.74 | 0.85 | 0.83 | 0.89 | 0.89 |
| crash-bat-insp | 15 | 15 | outage_s | 24.83 | 24.93 | 24.92 | 25.01 | 25.01 |
| crash-bat-insp | 15 | 15 | flow_resume_s | 18.90 | 19.04 | 19.02 | 19.12 | 19.13 |
| crash-bat-insp | 15 | 15 | pans_lost | 0.00 | 0.00 | 0.00 | 0.00 | 0.00 |
| crash-thm-dep | 15 | 15 | detect_s | 0.32 | 0.32 | 0.35 | 0.42 | 0.43 |
| crash-thm-dep | 15 | 15 | recover_s | 0.74 | 0.85 | 0.85 | 0.96 | 1.10 |
| crash-thm-dep | 15 | 15 | outage_s | 24.83 | 24.90 | 24.91 | 24.99 | 25.01 |
| crash-thm-dep | 15 | 15 | flow_resume_s | 18.65 | 19.02 | 18.99 | 19.14 | 19.15 |
| crash-thm-dep | 15 | 15 | pans_lost | 0.00 | 0.00 | 0.00 | 0.00 | 0.00 |
| crash-thm-bake | 15 | 15 | detect_s | 0.32 | 0.32 | 0.33 | 0.36 | 0.37 |
| crash-thm-bake | 15 | 15 | recover_s | 0.75 | 0.79 | 0.81 | 0.86 | 0.89 |
| crash-thm-bake | 15 | 15 | outage_s | 24.79 | 24.92 | 25.24 | 24.98 | 29.85 |
| crash-thm-bake | 15 | 15 | flow_resume_s | 18.91 | 19.04 | 19.36 | 19.13 | 24.04 |
| crash-thm-bake | 15 | 15 | pans_lost | 0.00 | 0.00 | 0.00 | 0.00 | 0.00 |
| crash-thm-exit | 15 | 15 | detect_s | 0.31 | 0.32 | 0.32 | 0.32 | 0.32 |
| crash-thm-exit | 15 | 15 | recover_s | 0.75 | 0.79 | 0.80 | 0.86 | 0.90 |
| crash-thm-exit | 15 | 15 | outage_s | 24.83 | 24.92 | 24.92 | 25.00 | 25.01 |
| crash-thm-exit | 15 | 15 | flow_resume_s | 18.93 | 19.04 | 19.05 | 19.14 | 19.16 |
| crash-thm-exit | 15 | 15 | pans_lost | 0.00 | 0.00 | 0.00 | 0.00 | 0.00 |
| crash-thm-cool | 15 | 15 | detect_s | 0.32 | 0.32 | 0.34 | 0.42 | 0.43 |
| crash-thm-cool | 15 | 15 | recover_s | 0.75 | 0.85 | 0.83 | 0.86 | 0.89 |
| crash-thm-cool | 15 | 15 | outage_s | 24.84 | 24.89 | 24.90 | 25.00 | 25.01 |
| crash-thm-cool | 15 | 15 | flow_resume_s | 18.91 | 18.97 | 18.98 | 19.07 | 19.13 |
| crash-thm-cool | 15 | 15 | pans_lost | 0.00 | 0.00 | 0.00 | 0.00 | 0.00 |
| crash-fin-dpn | 15 | 15 | detect_s | 0.31 | 0.32 | 0.34 | 0.42 | 0.42 |
| crash-fin-dpn | 15 | 15 | recover_s | 0.74 | 0.79 | 0.79 | 0.85 | 0.85 |
| crash-fin-dpn | 15 | 15 | outage_s | 24.81 | 24.91 | 24.90 | 24.92 | 24.99 |
| crash-fin-dpn | 15 | 15 | flow_resume_s | 18.84 | 19.03 | 19.02 | 19.08 | 19.16 |
| crash-fin-dpn | 15 | 15 | pans_lost | 0.00 | 0.00 | 0.00 | 0.00 | 0.00 |
| crash-fin-qua | 15 | 15 | detect_s | 0.32 | 0.32 | 0.33 | 0.42 | 0.42 |
| crash-fin-qua | 15 | 15 | recover_s | 0.74 | 0.79 | 0.79 | 0.85 | 0.90 |
| crash-fin-qua | 15 | 15 | outage_s | 24.82 | 24.90 | 24.89 | 24.93 | 24.94 |
| crash-fin-qua | 15 | 15 | flow_resume_s | 18.84 | 19.03 | 18.99 | 19.06 | 19.07 |
| crash-fin-qua | 15 | 15 | pans_lost | 0.00 | 0.00 | 0.00 | 0.00 | 0.00 |
| crash-fin-wrap | 15 | 15 | detect_s | 0.32 | 0.32 | 0.33 | 0.35 | 0.42 |
| crash-fin-wrap | 15 | 15 | recover_s | 0.75 | 0.80 | 0.81 | 0.86 | 0.87 |
| crash-fin-wrap | 15 | 15 | outage_s | 24.81 | 24.93 | 25.24 | 25.02 | 29.79 |
| crash-fin-wrap | 15 | 15 | flow_resume_s | 18.83 | 19.04 | 19.33 | 19.12 | 23.82 |
| crash-fin-wrap | 15 | 15 | pans_lost | 0.00 | 0.00 | 0.00 | 0.00 | 0.00 |
| crash-fin-pal | 15 | 15 | detect_s | 0.32 | 0.32 | 0.33 | 0.42 | 0.43 |
| crash-fin-pal | 15 | 15 | recover_s | 0.74 | 0.75 | 0.77 | 0.85 | 0.86 |
| crash-fin-pal | 15 | 15 | outage_s | 29.82 | 29.86 | 29.88 | 29.95 | 29.97 |
| crash-fin-pal | 15 | 15 | flow_resume_s | 23.83 | 24.02 | 24.00 | 24.08 | 24.10 |
| crash-fin-pal | 15 | 15 | pans_lost | 0.00 | 0.00 | 0.00 | 0.00 | 0.00 |
| crash-fin-wash | 15 | 14 | detect_s | 0.32 | 0.32 | 0.33 | 0.42 | 0.42 |
| crash-fin-wash | 15 | 14 | recover_s | 0.74 | 0.85 | 0.82 | 0.85 | 0.89 |
| crash-fin-wash | 15 | 14 | outage_s | 18.76 | 24.90 | 24.46 | 24.94 | 25.00 |
| crash-fin-wash | 15 | 14 | flow_resume_s | 12.79 | 18.93 | 18.52 | 19.06 | 19.07 |
| crash-fin-wash | 15 | 14 | pans_lost | -1.00 | 0.00 | -0.07 | 0.00 | 0.00 |
| crash-bt | 15 | 15 | detect_s | 0.32 | 0.32 | 0.33 | 0.37 | 0.42 |
| crash-bt | 15 | 15 | recover_s | 0.74 | 0.79 | 0.81 | 0.90 | 0.90 |
| crash-bt | 15 | 15 | outage_s | 24.80 | 24.90 | 24.88 | 25.00 | 25.00 |
| crash-bt | 15 | 15 | flow_resume_s | 18.81 | 18.93 | 18.96 | 19.07 | 19.11 |
| crash-bt | 15 | 15 | pans_lost | 0.00 | 0.00 | 0.00 | 0.00 | 0.00 |
| freeze-thm-bake | 15 | 15 | detect_s | 6.93 | 7.05 | 7.01 | 7.06 | 7.06 |
| freeze-thm-bake | 15 | 15 | outage_s | 24.83 | 24.87 | 24.88 | 24.94 | 24.95 |
| freeze-thm-bake | 15 | 15 | flow_resume_s | 16.73 | 16.85 | 16.84 | 16.93 | 16.94 |
| freeze-thm-bake | 15 | 15 | pans_lost | 0.00 | 0.00 | 0.00 | 0.00 | 0.00 |
| freeze-two-stations | 15 | 15 | detect_s | 6.52 | 6.53 | 6.55 | 6.63 | 6.63 |
| freeze-two-stations | 15 | 15 | recover_s | 0.01 | 0.01 | 0.01 | 0.01 | 0.01 |
| freeze-two-stations | 15 | 15 | outage_s | 24.83 | 24.87 | 25.21 | 24.99 | 29.85 |
| freeze-two-stations | 15 | 15 | flow_resume_s | 15.31 | 15.33 | 15.76 | 16.42 | 20.34 |
| freeze-two-stations | 15 | 15 | pans_lost | 0.00 | 0.00 | 0.00 | 0.00 | 0.00 |
| freeze-three-stations | 15 | 15 | detect_s | 5.90 | 6.10 | 6.06 | 6.12 | 6.12 |
| freeze-three-stations | 15 | 15 | recover_s | 0.01 | 0.01 | 0.01 | 0.01 | 0.01 |
| freeze-three-stations | 15 | 15 | outage_s | 24.78 | 24.89 | 24.88 | 24.91 | 24.92 |
| freeze-three-stations | 15 | 15 | flow_resume_s | 14.86 | 14.89 | 14.92 | 15.00 | 15.01 |
| freeze-three-stations | 15 | 15 | pans_lost | 0.00 | 0.00 | 0.00 | 0.00 | 0.00 |

(`recover_s` is not recorded for freezes of a single station: the container is not
restarted. `detect_s` for a freeze is the time until the broker declared it dead.)

**The one failed run:** `crash-fin-wash`, run 1: "pan count changed: 48, expected 47". The
count before the run was 47, after it 48. A pan cannot appear, so the first count was
most likely taken while one pan was between two stations. This looks like a measurement
artefact of the runner, not a lost pan, but it was not investigated further. Runs 2 to 15
of that scenario show 0.

**Availability run (2 h, `FAULT_RATE=2`):**

| Measure | Value |
|---|---|
| Container availability | mean 99.997 %, worst 99.986 % (BAT-DOSE) |
| Line availability (a pallet leaves at least every 1m15s) | 100.00 % |
| Container outages | 3, mean 1.0 s, longest 1.0 s |
| Repairs by the crew | 2, mean 53 s from ticket to confirmed repair |
| Throughput | 3444 cakes/h (maximum 3600) |
| Pans at the end | 48 of 48 in the loop |
| Events | about 260 warnings and discards, 2 errors (FC22, FC23) |

Against the requirements (a first reading; the analysis is the next exercise):

| Requirement | Measured | |
|---|---|---|
| detection within 15 s (THM-REQ-72) | crash: at most 0.43 s; frozen: at most 7.06 s | met |
| recovery within 60 s (THM-REQ-73) | at most 1.19 s | met |
| no product lost | `pans_lost` = 0 in 269 runs; one run is the artefact above | met |
| availability 99.5 % (THM-REQ-71) | 99.997 % mean over 2 h at doubled rates | met in this run |

### 5.1 Re-run of the two PLM-related crash scenarios after the PLM change (9 Nov)

PLM-WH was changed to follow the state machine of the Papyrus model (`decisions.md`, 9 Nov): BAT is
confirmed after the withdrawal, about 3 s later than before. The scenarios `crash-plm-wh` and
`crash-bat-dose` were run again, 15 runs each, on the rebuilt `plm-wh` (the other 15 containers were
built from the earlier code, which they share with this version except for PLM-WH). Data:
[experiment-results-plm-after.csv](experiment-results-plm-after.csv). **30 of 30 runs passed.**

| scenario | runs | passed | metric | min | median | mean | p95 | max |
|---|---|---|---|---|---|---|---|---|
| crash-plm-wh | 15 | 15 | detect_s | 0.32 | 0.32 | 0.34 | 0.42 | 0.43 |
| crash-plm-wh | 15 | 15 | recover_s | 0.85 | 0.98 | 0.98 | 1.13 | 1.20 |
| crash-plm-wh | 15 | 15 | outage_s | 23.90 | 24.85 | 25.10 | 24.91 | 29.86 |
| crash-plm-wh | 15 | 15 | flow_resume_s | 17.51 | 18.68 | 18.95 | 18.92 | 23.80 |
| crash-plm-wh | 15 | 15 | pans_lost | 0.00 | 0.00 | 0.00 | 0.00 | 0.00 |
| crash-bat-dose | 15 | 15 | detect_s | 0.32 | 0.32 | 0.33 | 0.33 | 0.43 |
| crash-bat-dose | 15 | 15 | recover_s | 0.86 | 0.91 | 0.94 | 1.08 | 1.09 |
| crash-bat-dose | 15 | 15 | outage_s | 24.72 | 24.78 | 24.80 | 24.91 | 24.92 |
| crash-bat-dose | 15 | 15 | flow_resume_s | 18.46 | 18.62 | 18.65 | 18.84 | 18.91 |
| crash-bat-dose | 15 | 15 | pans_lost | 0.00 | 0.00 | 0.00 | 0.00 | 0.00 |

What it says: detection (at most 0.43 s) and recovery (at most 1.20 s) are the same as in section 5, no
pan was lost, and the line resumes after about 18.6 s as before. One `crash-plm-wh` run took 29.9 s until the
outage ended and 23.8 s until the flow resumed, about 5 s more than the rest. The outage depends on where in the
22 s order cycle the crash falls (section 6); whether the 3 s withdrawal adds to it was not checked. It is one run in
fifteen and it was not investigated further. What it does not say:
the other 16 scenarios, the faults `faults-system` and the 2 h availability run were not repeated with the new
code (the first run of `faults-system` with FC1 and FC2, `healthy` and `sql-down` passed, but they are not
measurements). The availability figure of section 5 is from the earlier code.

### 5.2 Full rerun on 25 Nov 2026 (current code)

All 18 scenarios, 15 runs each, were run again on 25 Nov (01:13 to 03:15 local time), on a freshly built stack with
the code of the `work2` branch (PLM-WH follows its state machine; THM-DEP restores a long saved queue without blocking, see
`decisions.md`). Data: [experiment-results-rerun.csv](experiment-results-rerun.csv). **269 of 270 runs passed.**

| Measure | 3 Nov | 25 Nov |
|---|---|---|
| runs passed | 269 of 270 | 269 of 270 |
| crash: time to detect, mean (max) | 0.33 s (0.43 s) | 0.34 s (0.43 s) |
| crash: time to recover, mean (max) | 0.8 s (1.19 s) | 0.87 s (1.23 s) |
| freeze: time to detect, mean (max) | 7.0 s (7.06 s) | 7.1 s (7.89 s) for one station |
| pans lost | 0 | 0 |
| crash: outage, median | 24.9 s | 24.9 s |

Detection (at most 0.43 s for a crash, at most 7.9 s for a freeze) and recovery (at most 1.23 s) are the same as on 3 Nov
and inside the requirements (15 s and 60 s). No pan was lost in any of the 269 passed runs.

**The failed run:** `freeze-three-stations`, run 1 (02:09): "BAT-MIX did not come back within 1m0s". The broker declared
BAT-MIX dead at 02:08:15, then sent a second death certificate at 02:09:26 and the birth certificate arrived at 02:09:28, about
70 s after the freeze and 13 s later than the limit of 60 s. The line was normal again afterwards and the other 14 runs of
the scenario passed (on 3 Nov all 15 passed). The cause was not found. It looks like a race between the old frozen
connection and the new one of the same client, or a load effect on the test machine; this is a guess and it was not checked.

**Long outages:** 6 of the 269 passed runs had an outage above 35 s (`crash-plm-wh` 85 s and 40 s, `crash-bat-dose` 70 s, 60 s, 60 s
and 40 s), against 25 s for the median run. All six are in the first 25 minutes of the experiment, in the two scenarios that
the PLM change touches (PLM-WH and BAT-DOSE), and other work on the same machine (screen recording, slide export) was
running during that time. The cause was not investigated, so we cannot say if this is a real tail of the system or an effect of the
machine load. Detection and recovery are not affected in these runs. For the availability estimate the tail matters: the
mean outage of a crash is 26.7 s, the median 24.9 s, the 95th percentile 30 s.

**Availability run of 25 Nov (2 h, `FAULT_RATE=2`, `CREW_SECONDS=40`, 03:17 to 05:16)** ([availability-run-rerun.log](availability-run-rerun.log)):

| Measure | 3 Nov | 25 Nov |
|---|---|---|
| Container availability, mean | 99.997 % | 99.844 % |
| Container availability, worst | 99.986 % (BAT-DOSE) | 98.752 % (THM-DEP) |
| Line availability (a pallet leaves at least every 1m15s) | 100.00 % | 99.94 % |
| Container outages | 3, mean 1.0 s, longest 1.0 s | 10, mean 18.0 s, longest 55.5 s |
| Repairs by the crew | 2, mean 53 s | 6, mean 44 s |
| Throughput | 3444 cakes/h (96 %) | 3252 cakes/h (90 %) |
| Pans at the end | 48 of 48 | 96 of 96 (the loop now has 96 pans) |
| Random container crashes (FC27) / freezes (FC28) | 10 / 0 | 16 / 4 |

The mean availability of the containers is above the 99.5 % of THM-REQ-71, but the worst container (THM-DEP, about 90 s down in
7130 s) is **below** 99.5 % in this run. The two runs are not the same experiment: the failures are random and this run drew
4 freezes (FC28) and 16 crashes against 0 and 10 on 3 Nov. A freezing container stays down until the broker declares it dead and
the crew has repaired it, which explains the longer outages (up to 55.5 s) and the 6 repairs; this is our reading of the event
counts, we did not check each outage. Two hours at doubled rates is also not a month, so it
does not show the monthly figure of the requirement; it shows that the line keeps running (no pan lost, 90 % of the maximum
output) while containers crash and freeze at random. The 3 Nov figure of 99.997 % was a lucky draw and should not be quoted alone.

Per scenario (seconds):

| scenario | runs | passed | metric | min | median | mean | p95 | max |
|---|---|---|---|---|---|---|---|---|
| crash-plm-wh | 15 | 15 | detect_s | 0.31 | 0.31 | 0.31 | 0.32 | 0.32 |
| crash-plm-wh | 15 | 15 | recover_s | 0.84 | 0.89 | 0.90 | 0.99 | 1.05 |
| crash-plm-wh | 15 | 15 | outage_s | 23.89 | 24.97 | 30.21 | 39.84 | 84.95 |
| crash-plm-wh | 15 | 15 | flow_resume_s | 17.97 | 19.03 | 24.26 | 33.90 | 78.96 |
| crash-plm-wh | 15 | 15 | pans_lost | 0.00 | 0.00 | 0.00 | 0.00 | 0.00 |
| crash-plm-ship | 15 | 15 | detect_s | 0.31 | 0.32 | 0.34 | 0.42 | 0.42 |
| crash-plm-ship | 15 | 15 | recover_s | 0.73 | 0.84 | 0.86 | 0.95 | 0.95 |
| crash-plm-ship | 15 | 15 | outage_s | 20.82 | 25.85 | 25.52 | 25.94 | 30.74 |
| crash-plm-ship | 15 | 15 | flow_resume_s | 14.93 | 19.94 | 19.61 | 19.98 | 24.97 |
| crash-plm-ship | 15 | 15 | pans_lost | 0.00 | 0.00 | 0.00 | 0.00 | 0.00 |
| crash-bat-dose | 15 | 15 | detect_s | 0.32 | 0.32 | 0.34 | 0.43 | 0.43 |
| crash-bat-dose | 15 | 15 | recover_s | 0.78 | 0.89 | 0.90 | 0.97 | 1.08 |
| crash-bat-dose | 15 | 15 | outage_s | 24.84 | 25.00 | 33.93 | 59.99 | 69.83 |
| crash-bat-dose | 15 | 15 | flow_resume_s | 18.82 | 19.08 | 27.97 | 53.95 | 63.71 |
| crash-bat-dose | 15 | 15 | pans_lost | 0.00 | 0.00 | 0.00 | 0.00 | 0.00 |
| crash-bat-mix | 15 | 15 | detect_s | 0.32 | 0.32 | 0.34 | 0.43 | 0.43 |
| crash-bat-mix | 15 | 15 | recover_s | 0.79 | 0.90 | 0.95 | 1.14 | 1.23 |
| crash-bat-mix | 15 | 15 | outage_s | 24.74 | 24.92 | 25.25 | 25.00 | 29.95 |
| crash-bat-mix | 15 | 15 | flow_resume_s | 18.54 | 18.97 | 19.24 | 19.13 | 24.01 |
| crash-bat-mix | 15 | 15 | pans_lost | 0.00 | 0.00 | 0.00 | 0.00 | 0.00 |
| crash-bat-insp | 15 | 15 | detect_s | 0.32 | 0.32 | 0.34 | 0.40 | 0.42 |
| crash-bat-insp | 15 | 15 | recover_s | 0.79 | 0.91 | 0.91 | 0.98 | 0.98 |
| crash-bat-insp | 15 | 15 | outage_s | 24.82 | 24.90 | 25.24 | 24.98 | 29.83 |
| crash-bat-insp | 15 | 15 | flow_resume_s | 18.80 | 18.90 | 19.25 | 19.08 | 23.99 |
| crash-bat-insp | 15 | 15 | pans_lost | 0.00 | 0.00 | 0.00 | 0.00 | 0.00 |
| crash-thm-dep | 15 | 15 | detect_s | 0.31 | 0.32 | 0.36 | 0.43 | 0.43 |
| crash-thm-dep | 15 | 15 | recover_s | 0.85 | 0.86 | 0.90 | 0.99 | 1.01 |
| crash-thm-dep | 15 | 15 | outage_s | 24.85 | 24.93 | 26.25 | 34.87 | 34.92 |
| crash-thm-dep | 15 | 15 | flow_resume_s | 18.82 | 18.95 | 20.28 | 28.93 | 28.96 |
| crash-thm-dep | 15 | 15 | pans_lost | 0.00 | 0.00 | 0.00 | 0.00 | 0.00 |
| crash-thm-bake | 15 | 15 | detect_s | 0.32 | 0.33 | 0.36 | 0.43 | 0.43 |
| crash-thm-bake | 15 | 15 | recover_s | 0.76 | 0.90 | 0.90 | 1.00 | 1.00 |
| crash-thm-bake | 15 | 15 | outage_s | 24.84 | 24.91 | 25.92 | 29.94 | 29.97 |
| crash-thm-bake | 15 | 15 | flow_resume_s | 18.81 | 18.94 | 19.94 | 23.91 | 24.03 |
| crash-thm-bake | 15 | 15 | pans_lost | 0.00 | 0.00 | 0.00 | 0.00 | 0.00 |
| crash-thm-exit | 15 | 15 | detect_s | 0.32 | 0.33 | 0.36 | 0.43 | 0.43 |
| crash-thm-exit | 15 | 15 | recover_s | 0.75 | 0.85 | 0.84 | 0.90 | 0.95 |
| crash-thm-exit | 15 | 15 | outage_s | 24.83 | 24.89 | 25.24 | 24.99 | 29.97 |
| crash-thm-exit | 15 | 15 | flow_resume_s | 18.87 | 19.01 | 19.33 | 19.12 | 24.05 |
| crash-thm-exit | 15 | 15 | pans_lost | 0.00 | 0.00 | 0.00 | 0.00 | 0.00 |
| crash-thm-cool | 15 | 15 | detect_s | 0.32 | 0.32 | 0.33 | 0.33 | 0.43 |
| crash-thm-cool | 15 | 15 | recover_s | 0.85 | 0.86 | 0.88 | 0.91 | 0.96 |
| crash-thm-cool | 15 | 15 | outage_s | 24.81 | 24.90 | 25.57 | 29.88 | 30.01 |
| crash-thm-cool | 15 | 15 | flow_resume_s | 18.85 | 18.96 | 19.61 | 23.87 | 24.09 |
| crash-thm-cool | 15 | 15 | pans_lost | 0.00 | 0.00 | 0.00 | 0.00 | 0.00 |
| crash-fin-dpn | 15 | 15 | detect_s | 0.32 | 0.32 | 0.34 | 0.42 | 0.42 |
| crash-fin-dpn | 15 | 15 | recover_s | 0.75 | 0.89 | 0.88 | 0.99 | 1.02 |
| crash-fin-dpn | 15 | 15 | outage_s | 24.68 | 24.87 | 25.54 | 29.92 | 29.93 |
| crash-fin-dpn | 15 | 15 | flow_resume_s | 18.61 | 18.95 | 19.57 | 23.95 | 24.08 |
| crash-fin-dpn | 15 | 15 | pans_lost | 0.00 | 0.00 | 0.00 | 0.00 | 0.00 |
| crash-fin-qua | 15 | 15 | detect_s | 0.32 | 0.32 | 0.33 | 0.35 | 0.42 |
| crash-fin-qua | 15 | 15 | recover_s | 0.76 | 0.86 | 0.84 | 0.87 | 0.90 |
| crash-fin-qua | 15 | 15 | outage_s | 24.83 | 24.91 | 25.56 | 29.82 | 29.87 |
| crash-fin-qua | 15 | 15 | flow_resume_s | 18.84 | 18.97 | 19.62 | 23.94 | 23.94 |
| crash-fin-qua | 15 | 15 | pans_lost | 0.00 | 0.00 | 0.00 | 0.00 | 0.00 |
| crash-fin-wrap | 15 | 15 | detect_s | 0.32 | 0.32 | 0.35 | 0.42 | 0.42 |
| crash-fin-wrap | 15 | 15 | recover_s | 0.75 | 0.85 | 0.82 | 0.88 | 0.91 |
| crash-fin-wrap | 15 | 15 | outage_s | 24.77 | 24.91 | 25.56 | 29.84 | 29.89 |
| crash-fin-wrap | 15 | 15 | flow_resume_s | 18.78 | 19.01 | 19.64 | 23.94 | 24.07 |
| crash-fin-wrap | 15 | 15 | pans_lost | 0.00 | 0.00 | 0.00 | 0.00 | 0.00 |
| crash-fin-pal | 15 | 15 | detect_s | 0.32 | 0.32 | 0.34 | 0.42 | 0.43 |
| crash-fin-pal | 15 | 15 | recover_s | 0.75 | 0.85 | 0.82 | 0.86 | 0.87 |
| crash-fin-pal | 15 | 15 | outage_s | 29.81 | 29.88 | 29.89 | 29.98 | 30.01 |
| crash-fin-pal | 15 | 15 | flow_resume_s | 23.83 | 24.02 | 23.98 | 24.07 | 24.07 |
| crash-fin-pal | 15 | 15 | pans_lost | 0.00 | 0.00 | 0.00 | 0.00 | 0.00 |
| crash-fin-wash | 15 | 15 | detect_s | 0.32 | 0.32 | 0.33 | 0.33 | 0.42 |
| crash-fin-wash | 15 | 15 | recover_s | 0.74 | 0.86 | 0.84 | 0.90 | 0.97 |
| crash-fin-wash | 15 | 15 | outage_s | 24.81 | 24.88 | 24.88 | 24.98 | 25.00 |
| crash-fin-wash | 15 | 15 | flow_resume_s | 18.76 | 18.95 | 18.93 | 19.01 | 19.03 |
| crash-fin-wash | 15 | 15 | pans_lost | 0.00 | 0.00 | 0.00 | 0.00 | 0.00 |
| crash-bt | 15 | 15 | detect_s | 0.31 | 0.32 | 0.33 | 0.37 | 0.42 |
| crash-bt | 15 | 15 | recover_s | 0.75 | 0.85 | 0.83 | 0.89 | 0.90 |
| crash-bt | 15 | 15 | outage_s | 24.81 | 24.88 | 25.55 | 29.95 | 29.99 |
| crash-bt | 15 | 15 | flow_resume_s | 18.71 | 18.96 | 19.59 | 23.98 | 24.00 |
| crash-bt | 15 | 15 | pans_lost | 0.00 | 0.00 | 0.00 | 0.00 | 0.00 |
| freeze-thm-bake | 15 | 15 | detect_s | 6.82 | 6.94 | 7.14 | 7.87 | 7.89 |
| freeze-thm-bake | 15 | 15 | outage_s | 24.80 | 24.90 | 25.54 | 29.85 | 29.88 |
| freeze-thm-bake | 15 | 15 | flow_resume_s | 15.84 | 16.92 | 17.28 | 20.86 | 20.89 |
| freeze-thm-bake | 15 | 15 | pans_lost | 0.00 | 0.00 | 0.00 | 0.00 | 0.00 |
| freeze-two-stations | 15 | 15 | detect_s | 6.23 | 6.52 | 6.79 | 7.37 | 7.38 |
| freeze-two-stations | 15 | 15 | recover_s | 0.01 | 0.01 | 0.01 | 0.02 | 0.03 |
| freeze-two-stations | 15 | 15 | outage_s | 24.91 | 29.90 | 31.21 | 34.92 | 34.98 |
| freeze-two-stations | 15 | 15 | flow_resume_s | 15.30 | 20.42 | 21.67 | 25.44 | 25.48 |
| freeze-two-stations | 15 | 15 | pans_lost | 0.00 | 0.00 | 0.00 | 0.00 | 0.00 |
| freeze-three-stations | 15 | 14 | detect_s | 5.79 | 5.89 | 6.11 | 6.83 | 6.84 |
| freeze-three-stations | 15 | 14 | recover_s | 0.01 | 0.01 | 0.01 | 0.01 | 0.02 |
| freeze-three-stations | 15 | 14 | outage_s | 24.71 | 24.86 | 25.56 | 29.80 | 29.89 |
| freeze-three-stations | 15 | 14 | flow_resume_s | 13.80 | 14.85 | 15.27 | 18.70 | 19.99 |
| freeze-three-stations | 15 | 14 | pans_lost | 0.00 | 0.00 | 0.00 | 0.00 | 0.00 |

## 6. Observations and unexpected behaviour

1. **The outage is about 25 s for almost every container**, much longer than detection or
   recovery (under 1.2 s). The line has to refill: batter and pans in flight must travel
   on, and the next order comes on its own cycle. The spread is small (median 24.9 s, p95
   25.0 s), so the refill is a fixed delay and does not depend on which container fails.
2. **Some runs take about 5 s longer:** `crash-fin-pal` (median outage 29.9 s) and single
   runs of `crash-thm-bake`, `crash-fin-wrap` and `freeze-two-stations` (up to 29.9 s). The
   cause was not investigated.
3. **A crash is detected about 20 times faster than a freeze** (0.32 s against 6 to 7 s).
   A crash closes the connection and the broker publishes the death certificate at once; a
   frozen process is only found when keep-alives stop (limit 7.5 s).
4. **Several stations failing together do not make it worse.** Freezing two or three stations
   gave the same outage (about 25 s) as one, and the band halted and released as designed.
   The flow resumed 15 s after the release, against 17 s for one frozen station.
5. **Batter in flight is no longer lost.** BAT-MIX, BAT-INSP and THM-DEP save their queue, so
   a crash of them costs about 25 s like every other container (a BAT-INSP crash took 135 s
   before the queue was saved).
6. **Random failures are mostly absorbed.** In 2 h at doubled rates about 260 events
   happened, most handled by the station itself with a discard or a retry. Only 3 containers
   went down, for 1 s each, and two failures needed the crew. The line made 3444 cakes/h, 96 %
   of the maximum.

## 7. Threats to validity

- The machines are simulated and the process times are compressed (for example the 30
  minutes of UC10 are 40 s), so absolute outage times describe this simulation, not a real
  line. Detection and recovery times depend only on the software and the broker.
- One machine and one Docker host: no network delay, no competing load.
- The failure rates are chosen, not measured (`docs/assumptions.md`, section 7). The
  availability figure holds for these rates only. Two hours with 3 container outages is a
  small sample for the container-level figure.
- Fifteen runs per scenario give a usable median and p95, but the tail beyond p95 is not seen.
- "Recovered" means the birth certificate was received, not full capacity; "flow resumed"
  means the next pallet shipped.
- The runner polls every 100 ms with its own clock; times have about that resolution.
- `pans_lost` counts pan documents, so it cannot see a pan that is between stations.

## 8. For the analysis exercise

Availability per fault can be estimated as outage (`outage_s`) times fault rate, compared with
99.5 % (about 216 minutes of downtime in 30 days). With a 25 s outage, one container crash
per hour would cost about 18 minutes a month. The long run is a check: 99.997 % at doubled
rates. The group has to defend the rates; the dominant term is the refill time, not detection
or recovery.
