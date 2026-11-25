# The experiment explained (for questions)

Everything here comes from [experiment.md](../experiment.md) (exercise 7.1) and the runner code in `internal/scenario/scenario.go`.
Numbers are from the full rerun on 25 Nov 2026 unless it says 3 Nov.

## 1. The idea in one minute

We want to know: **if one container of the factory fails, how fast do we notice, how fast is it back, how long does production stop,
and do we lose product?** That is the quality attribute **availability**. We break each container on purpose, many times, and measure.

The requirements we compare with (from the THM document):

| Requirement | Meaning | Our metric |
|---|---|---|
| THM-REQ-71 | 99.5 % availability per month | availability run (part 2) |
| THM-REQ-72 | a stopped controller is detected within 15 s (3 heartbeats of 5 s) | `detect_s` |
| THM-REQ-73 | back in normal operation within 60 s | `recover_s` |

## 2. How the experiment is built (GQM)

We used the GQM template (Goal, Question, Metric):

- **Goal:** analyse the Docker deployment of the cake factory, to evaluate availability, from the point of view of the production overseer.
- **Questions and metrics** (all measured by a program, with a resolution of about 100 ms):

| Question | Metric | How it is measured |
|---|---|---|
| How fast is a failed system noticed? | `detect_s` | from fault injection until MON has the death certificate (NDEATH) |
| How fast is it back? | `recover_s` | from fault injection until the birth certificate (NBIRTH) arrives |
| How long does production stop? | `outage_s` | from fault injection until the next cake ships (a pallet leaves) |
| How long until the line flows again? | `flow_resume_s` | from the system being up until flow resumes |
| Is product lost? | `pans_lost` | pans counted in MongoDB before the fault and after recovery; expected 0 |

## 3. Variables

- **What we change (independent variable):** which container fails (each of the 14 stations and BT, one at a time) and how it fails (crash, freeze, several frozen together).
- **What we keep fixed:** heartbeat 5 s, keep-alive 5 s, real-time speed, one recipe, 48 pans in the loop, 24 pans per batch, no random failures, and the line settled
  (every container online, no open error) before each run. This is so that a difference in the result comes from the fault and not from something else.
- **Repetitions:** 15 runs per scenario, 18 scenarios, 270 runs. Many runs because a single run can be a lucky or unlucky one.

## 4. The 18 scenarios

- **15 crashes**, one for each container: `crash-plm-wh`, `crash-plm-ship`, `crash-bat-dose`, `crash-bat-mix`, `crash-bat-insp`, `crash-thm-dep`, `crash-thm-bake`,
  `crash-thm-exit`, `crash-thm-cool`, `crash-fin-dpn`, `crash-fin-qua`, `crash-fin-wrap`, `crash-fin-pal`, `crash-fin-wash`, `crash-bt`.
  A crash kills the process. The broker publishes the death certificate at once. Docker restarts the container.
- **3 freezes** (`docker pause`, so no keep-alive pings): `freeze-thm-bake` (the baking tunnel alone), `freeze-two-stations` (THM-BAKE and FIN-WRAP together; the whole band halts),
  `freeze-three-stations` (THM-EXIT, THM-COOL, BAT-MIX together; sections 1 to 3 halt, 4 and 5 keep running).
- MON is not crashed: it is the observer.
- Why both: a crash and a freeze are different mechanisms. A crash is seen at once (the connection closes). A freeze is only found when the keep-alive pings stop.

## 5. What happens in one run (the procedure)

1. Start from a settled factory: every container online, no open error.
2. Record the number of pans.
3. Inject the fault through the dashboard's API (`/api/inject`). The runner uses the same API as the dashboard, so it measures what an overseer would see.
4. Poll the event log every 100 ms and note the time of each event.
5. **Crash:** MON receives the death certificate (detect). Docker restarts the container (about 0.8 s). The station reads its saved state and sends the birth certificate (recover).
   **Freeze:** the broker closes the connection after 7.5 s and publishes the death certificate (detect). The runner then runs `docker unpause`; the station reconnects and sends the birth certificate.
6. Wait until the line is settled again. Count the pans again (`pans_lost`). Wait for the next pallet to ship (`outage_s`).
7. Write one row to the CSV.

Reproduce: `docker compose up -d --build`, then `go run ./cmd/scenario experiment -runs 15 -out docs/experiment-results.csv`.

## 6. Second part: availability run

A single fault cannot show the monthly availability. So the factory runs **2 hours with random failures** (`FAULT_RATE=2`: all failure rates doubled, so 2 h equal 4 h at the catalogue rates)
and a simulated maintenance crew (`CREW_SECONDS=40`: repairs each ticket after a random time of about 40 s). Nothing is injected by hand.
Container availability is computed from the time each container was down.

## 7. Results (full rerun, 25 Nov 2026)

| Question | Result | Requirement |
|---|---|---|
| Detect a crash | at most 0.34 s on average, max 0.43 s | 15 s: met |
| Detect a freeze | about 6 to 8 s (broker limit 7.5 s) | 15 s: met |
| Recover from a crash | at most 1.23 s | 60 s: met |
| Outage (until next cake ships) | about 25 s (the line has to refill; a few scenarios 30 s) | no requirement |
| Product lost | 0 pans in all passed runs | 0: met |
| Container availability, 2 h run | 99.844 % on average; the worst container 98.752 % | 99.5 %: the average is above, the worst container is below |
| Throughput | 3252 cakes/h (maximum 3600) | none |

Passed runs: 269 of 270, in both the 3 Nov run and the 25 Nov rerun, but not the same one failing (see section 8).

Why the outage (about 25 s) is longer than the recovery (about 1 s): the container is back in 1 s, but the batter and pans that were in the line have to move on,
and the next pallet of 24 cakes has to be completed and shipped. The outage is the time to refill the line, not the time to restart.

## 8. Failed runs and things that differ (they may ask)

- **3 Nov:** one run failed: `crash-fin-wash`, run 1, "pan count changed: 48, expected 47". A pan cannot appear, so most likely the count was taken while one pan was between two stations.
  It looks like a measurement artefact of the runner, not a lost pan. Not investigated further. The other 14 runs of that scenario show 0.
- **25 Nov rerun:** one run failed: `freeze-three-stations`, run 1: BAT-MIX did not come back within 60 s (it came back after about 70 s). The broker sent a second death certificate and
  the birth certificate came about 13 s after the limit. The line was normal afterwards and the other 14 runs passed. **Cause not found.** Our guess (not checked): a race between
  the old frozen connection and the new one of the same client, or load on the test machine.
- **Long outages in the rerun:** six outages were longer than usual (up to about 35 s). Possible cause: load on the machine. Not checked.
- **Availability 99.997 % on 3 Nov versus 99.844 % on 25 Nov:** the 3 Nov figure was a lucky draw. In the rerun the random run had 16 crashes and 4 freezes (3 Nov: 10 and 0). A freezing container stays down
  until the broker declares it dead (about 7 s), so freezes cost more. Do not quote the 99.997 % alone; say "99.8 %, and it varies from run to run".
- **Worst container 98.752 %** is below the 99.5 % requirement. We say it openly: the requirement is for the controllers on average over a month, and a 2 h run with doubled failure rates is harsher than the requirement assumes.
- **Why the THM-DEP fix mattered:** the first rerun was blocked because THM-DEP restored its queue in a way that could block at start. We changed it to a non-blocking restore (a batter of unknown age counts as expired).

## 9. Honest limits (say them before they ask)

- The failures are injected by us: the rates are our choice, not measured in a real plant.
- Everything runs on one machine in Docker. Network failures between machines are not tested.
- We only fail the factory containers. The broker, SQL Server and MongoDB are not failed.
- For freezes, the runner unfreezes the container. In a real plant a health check or a person would restart it.
- 15 runs per scenario is a small sample. The results were stable (the spread is small), but we make no statistics claim.
- Time is real time (`SIM_SPEED=1`), but machine times are compressed.

## 10. Questions they may ask

**Why 15 runs and 18 scenarios?**
One run can be lucky. 15 repetitions give a mean, a spread and a worst case. 18 scenarios cover every container once (15) plus the three kinds of freeze.

**Why did you not test X (the broker, a network cut, a disk full)?**
We chose the failure of a factory container because the requirements THM-REQ-71 to 73 are about controllers. Broker and database failure are listed as the next step.

**How do you know the measurement is right?**
The runner reads the same event log and API the dashboard uses, and polls every 100 ms, so the resolution is about 0.1 s. For `pans_lost` it counts the pan documents before and after.
One failure of the check on 3 Nov was a counting artefact, which we state.

**Why is the outage 25 s if recovery is 1 s?**
Because the line has to refill. See section 7.

**Why 99.5 % requirement and you got 98.752 % for one container?**
The requirement is an average per month; the 2 h run is one short sample with doubled failure rates. The mean (99.844 %) is above. We report the worst container honestly and do not claim a pass for it.

**What did you learn or change after the experiment?**
The THM-DEP queue restore fix (non-blocking restore). The finding that a batter waiting 25 s expires, which UPPAAL also showed.

**Is it reproducible?**
Yes: the commands in section 5, the CSV and the logs are in the repository (`docs/experiment-results-rerun.csv`, `rerun.log`, `availability-run-rerun.log`).
