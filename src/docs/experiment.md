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
> MongoDB) running in Docker Compose on one machine.

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

RESULTS_PLACEHOLDER
