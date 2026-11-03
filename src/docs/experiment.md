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
