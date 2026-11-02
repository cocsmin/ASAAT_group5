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
> **in the context of** the simulated line (five systems, MQTT broker, SQL Server and
> MongoDB; Band Transport became a sixth system after the run) running in Docker Compose on one machine.

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

- **Independent variables:** the system that fails (PLM, BAT, THM, FIN) and the way it
  fails (process crash; frozen process for THM).
- **Fixed:** heartbeat 5 s, MQTT keep-alive 5 s, `SIM_SPEED=1` (real time), one
  recipe, 20 pans, pallets of 6, no other fault injected, the line settled (every
  system online, no open error) before each run.
- **Replications:** 10 runs per scenario, 50 runs in total, to see variation; a
  pilot is not used because the first full run already shows the spread.
- **Procedure per run:** wait until the factory is settled; record the pan count; inject
  the fault through the monitor's API; poll the monitor's event log every 100 ms;
  record the times; wait until the line is settled; record the pan count and wait for
  the next pallet.
- **Instrumentation:** the experiment runner (`cmd/scenario experiment`) uses the
  monitor's own API (`/api/inject`, `/api/events`, `/api/state`,
  `/api/pans/summary`), so it measures what an overseer would see. Times are taken by
  the runner's clock, resolution about 100 ms.
- **Environment:** Docker Desktop on Windows 11, containers as in
  `docker-compose.yml`, started fresh (`down -v`) before the experiment.

Reproduce:

```bash
docker compose --profile demo up -d --build        # wait until all containers are healthy
go run ./cmd/scenario experiment -runs 10 -out docs/experiment-results.csv
go run ./cmd/scenario/summary docs/experiment-results.csv
```

Why this design: the scenarios are the faults the report names (FC27, FC28), they hit
each system once, and the runner decides pass or fail by the same requirements the
report states. Crash and freeze are different mechanisms (the broker's Last Will
fires at once on a crash; a frozen process is only found when keep-alives stop), so
both are included.

## 4. Data

`docs/experiment-results.csv`, one row per run:
`timestamp, scenario, run, result, detect_s, recover_s, outage_s, flow_resume_s, pans_lost, error`.

## 5. Results

Run on 2 Nov 2026, Docker Desktop on Windows 11, `SIM_SPEED=1`, fresh start, 50 runs
(10 per scenario), **50 of 50 completed and passed**. Raw data:
[experiment-results.csv](experiment-results.csv). Times in seconds.

| scenario | runs | passed | metric | min | median | mean | p95 | max |
|---|---|---|---|---|---|---|---|---|
| crash-plm | 10 | 10 | detect_s | 0.31 | 0.41 | 0.38 | 0.41 | 0.41 |
| crash-plm | 10 | 10 | recover_s | 0.72 | 0.82 | 0.77 | 0.82 | 0.82 |
| crash-plm | 10 | 10 | outage_s | 10.76 | 15.79 | 20.78 | 50.75 | 50.75 |
| crash-plm | 10 | 10 | flow_resume_s | 4.93 | 10.05 | 15.00 | 45.02 | 45.02 |
| crash-plm | 10 | 10 | pans_lost | 0.00 | 0.00 | 0.00 | 0.00 | 0.00 |
| crash-bat | 10 | 10 | detect_s | 0.31 | 0.31 | 0.35 | 0.41 | 0.41 |
| crash-bat | 10 | 10 | recover_s | 0.72 | 0.82 | 0.78 | 0.82 | 0.82 |
| crash-bat | 10 | 10 | outage_s | 14.96 | 44.97 | 36.98 | 59.93 | 59.93 |
| crash-bat | 10 | 10 | flow_resume_s | 9.13 | 39.22 | 31.19 | 54.20 | 54.20 |
| crash-bat | 10 | 10 | pans_lost | 0.00 | 0.00 | 0.00 | 0.00 | 0.00 |
| crash-thm | 10 | 10 | detect_s | 0.31 | 0.41 | 0.38 | 0.41 | 0.41 |
| crash-thm | 10 | 10 | recover_s | 0.72 | 0.72 | 0.74 | 0.82 | 0.82 |
| crash-thm | 10 | 10 | outage_s | 19.88 | 20.00 | 25.48 | 55.00 | 55.00 |
| crash-thm | 10 | 10 | flow_resume_s | 14.15 | 14.27 | 19.73 | 49.17 | 49.17 |
| crash-thm | 10 | 10 | pans_lost | 0.00 | 0.00 | 0.00 | 0.00 | 0.00 |
| crash-fin | 10 | 10 | detect_s | 0.31 | 0.41 | 0.38 | 0.41 | 0.41 |
| crash-fin | 10 | 10 | recover_s | 0.72 | 0.82 | 0.77 | 0.82 | 0.82 |
| crash-fin | 10 | 10 | outage_s | 14.97 | 20.00 | 21.48 | 40.02 | 40.02 |
| crash-fin | 10 | 10 | flow_resume_s | 9.13 | 14.27 | 15.70 | 34.29 | 34.29 |
| crash-fin | 10 | 10 | pans_lost | 0.00 | 0.00 | 0.00 | 0.00 | 0.00 |
| freeze-thm | 10 | 10 | detect_s | 4.70 | 6.64 | 6.48 | 7.67 | 7.67 |
| freeze-thm | 10 | 10 | outage_s | 19.98 | 25.02 | 24.98 | 29.95 | 29.95 |
| freeze-thm | 10 | 10 | flow_resume_s | 12.41 | 17.46 | 17.65 | 23.37 | 23.37 |
| freeze-thm | 10 | 10 | pans_lost | 0.00 | 0.00 | 0.00 | 0.00 | 0.00 |

(`recover_s` is not recorded for `freeze-thm`: the system is not restarted, it resumes
when it is unfrozen. `detect_s` for a freeze is the time until the broker declared it dead.)

Against the requirements (this is a first reading; the analysis is the next exercise):

| Requirement | Measured | |
|---|---|---|
| detection within 15 s (THM-REQ-72) | crash: at most 0.41 s; frozen process: at most 7.67 s | met |
| recovery within 60 s (THM-REQ-73) | at most 0.82 s (container restarted and reborn) | met |
| no product lost | `pans_lost` = 0 in all 50 runs | met |
| availability 99.5 % (THM-REQ-71) | not computed: needs a fault rate, see below | open |

## 6. Observations and unexpected behaviour

1. **The production outage is much longer than detection or recovery.** The system is
   back after under a second, but the next cake ships 10 to 60 s after the fault
   (median 15 to 45 s depending on the system). The line has to refill: orders and
   batter already in flight are lost with the crashed system, the next order arrives on
   its own cycle (every 22 s), and the cakes must travel through the tunnel again. BAT
   is the worst case (median 45 s, maximum 60 s) because the whole chain starts there.
   The spread inside one scenario (for example PLM 10.8 to 50.8 s) depends on where in
   the order cycle the fault lands.
2. **A crash is detected 15 to 20 times faster than a freeze** (0.3 to 0.4 s against
   4.7 to 7.7 s). A crash closes the connection and the broker publishes the death
   certificate at once; a frozen process is only found when keep-alives stop (limit
   1.5 x 5 s = 7.5 s; the shorter values come from where in the ping cycle the freeze
   happened).
3. **The first experiment run found a bug and was aborted.** After a PLM crash its order
   numbers restarted at PO-0001, BAT ignored the repeated ids and the line stalled for
   more than 3 minutes (run 3). Fixed (PLM continues after the highest stored order) and
   the experiment was repeated from scratch; the aborted data is kept in
   [experiment-aborted-run.csv](experiment-aborted-run.csv).
4. **Earlier, before the orders were sent again,** a BAT crash once took 95 s to
   resume. PLM now sends an order again after 40 s without an ingredient request.
5. **A crash costs at most one batch** (BAT-NFR-02); in these runs no pan was lost
   because cakes in flight are taken up again from the stored documents.

## 7. Threats to validity

- The machines are simulated and the process times are compressed (for example the
  30 minutes of UC10 are 40 s), so absolute outage times describe this simulation, not a
  real line. Detection and recovery times depend only on the software and the broker.
- One machine and one Docker host: no network delay, no competing load.
- 10 runs per scenario is small; the order cycle makes the outage times spread widely.
- "Recovered" means the birth certificate was received, not that the system is at full
  capacity; "flow resumed" means the next pallet shipped.
- The runner polls the monitor every 100 ms and uses its own clock, so times have about
  that resolution.
- Only two fault types (crash, freeze) and one fault at a time were tested; the other
  failure cases are covered by the scenarios in `docs/failure-cases.md`, not measured here.

## 8. For the analysis exercise

Availability per fault can be estimated as the outage (`outage_s`) times the fault rate
over a month, which then can be compared with 99.5 % (about 216 minutes of downtime in
30 days). The group has to choose the fault rate. With the medians above one fault per
day would cost a few minutes per month; the dominant term is the refill time, not
detection or recovery.

