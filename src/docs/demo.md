# Demo and reproducible checks

Everything that goes wrong in the report can be shown live and repeated by anyone.
There are two ways: click the failure case on the dashboard, or run a scenario.
A scenario does something bad to the running factory, prints a timeline of what
the factory does about it, and ends with PASS or FAIL. The same scenarios run in
the CI pipeline.

## From the dashboard

Two cards on the right side of the dashboard do this without a terminal:

- **Components** lists the broker, SQL Server, MongoDB and every factory container with a
  state and buttons: *Stop*, *Start*, *Freeze* (the process stops answering, like
  a hung machine), *Unfreeze*, *Restart*. Each has a line that says what to expect.
  The monitor can be switched off here too (the dashboard is a separate container
  and keeps showing what happens); only the dashboard itself is locked.
- **Test scenarios** lists the scenarios below with a *Run* button and a *Run all*
  button. The timeline appears live and ends with PASS or FAIL. Scenarios that
  switch containers need the control service. A *Stop* button ends a run and puts
  every stopped or frozen part back.

The control service is the `ctl` container, started with
`docker compose --profile demo up -d --build`.

## From a terminal

## Run it

1. Start the factory (Docker Desktop must be running):

   ```bash
   docker compose --profile demo up -d --build
   ```

   To make everything three times faster (shorter demos), use `SIM_SPEED=3`
   (in PowerShell: `$env:SIM_SPEED="3"; docker compose up -d --build`).
   Wait until `docker compose ps` shows every container as `healthy`.
2. Open the dashboard at <http://localhost:8080> and keep it next to the terminal.
3. Run scenarios:

   ```bash
   go run ./cmd/scenario list
   go run ./cmd/scenario healthy crash-thm-bake
   go run ./cmd/scenario all
   ```


## The scenarios

| Scenario | What happens to the factory | What it proves |
|---|---|---|
| `healthy` | nothing | all 16 factory containers online, a pallet ships |
| `crash-plm-wh`, `crash-plm-ship`, `crash-bat-dose`, `crash-bat-mix`, `crash-bat-insp`, `crash-thm-dep`, `crash-thm-bake`, `crash-thm-exit`, `crash-thm-cool`, `crash-fin-dpn`, `crash-fin-qua`, `crash-fin-wrap`, `crash-fin-pal`, `crash-fin-wash`, `crash-bt` | the container exits like a real crash | the broker publishes the death certificate (Last Will) within 15 s (THM-REQ-72); Docker restarts the container and it is back within 60 s (THM-REQ-73); no pan is lost or duplicated; the line carries on |
| `freeze-two-stations`, `freeze-three-stations` | the baking tunnel and the wrapping station freeze together; the tunnel exit, the cooling and the mixing station freeze together | each is declared dead, MON halts the union of their sections and everything upstream (the whole band in the first case, S1 to S3 in the second, mixing owns no section), they all return, the sections run again, no pan is lost |
| `freeze-thm-bake` | the baking tunnel station freezes and stops its keep-alive pings | the broker declares it dead on its own (about 7.5 s with a 5 s keep-alive), MON halts its band section and the ones upstream; it comes back when unfrozen |
| `mon-down` | MON is stopped while THM discards cakes | no event is lost (THM-REQ-77: the events wait for MON and arrive after it restarts); no new batch starts without the monitor and the line finishes what it has (UC2 rule, assumption S4) |
| `sql-down` | SQL Server is switched off for 30 s | the monitor does not mistake a database outage for a dead line (no node declared dead, no band section halted), stations carry on, everything catches up when it returns |
| `broker-restart` | the MQTT broker restarts, all connections drop | every node reconnects, repeats what was lost, the line continues |
| `cooling-chain` | cooling stops and nobody repairs it for a while | the chain of the report: the buffer fills and reports it (FC18), cakes that wait too long are discarded (FC17), the repair releases or refuses the buffered cakes (FC16) |
| `faults-system` | 16 failure cases that the system handles itself | each produces its event (warning or discard) and the line goes on |
| `faults-maintenance` | 11 failure cases that need Maintenance | each produces an error and a ticket; the scenario confirms the repair like Maintenance would, and the factory recovers |

The table of all 28 failure cases and how each is triggered is in
[failure-cases.md](failure-cases.md). Each failure case has the code of the report (FC1 to FC28, plus UC4, UC5, UC12,
UC14) so the output can be matched to the failure-case tables.

## A short live demo (about 5 minutes)

1. Dashboard open, the line running, "All clear".
2. Click `FC9` under THM ("wrong fill weight"): a warning shows, the pan is
   diverted, the discard counter goes up, and the line goes on (handled by the
   system, no ticket).
3. Click `FC5` under BAT ("dosing fails several times in a row"): the doser locks
   out, "Needs attention" shows a red entry and a Maintenance ticket appears.
   Press *Mark repaired*: the doser resumes by itself.
4. Click `FC19` under BT: a band section stops, upstream sections halt and
   downstream runs empty, the bar below the flow shows it. Repair it and watch the
   sections restart in order.
5. In a terminal: `go run ./cmd/scenario crash-thm-bake`, and say what the timeline
   shows: death certificate after 0.4 s, back after 0.7 s, 20 pans still in the
   loop, flow resumed.
6. Optional: `go run ./cmd/scenario mon-down` to show that no event is lost.

## Measured results

Run on Docker Desktop (Windows 11) with `SIM_SPEED=1` (real seconds), one container per station,
3 Nov 2026: **26 of 26 scenarios passed** (the first scenario of the list ran before the later ones
and the whole list took about 25 minutes). The numbers are one run, not a statistic; the
availability experiment with ten runs per scenario is in [experiment.md](experiment.md).

| Scenario | Result | Numbers |
|---|---|---|
| `healthy` | PASS | all containers online, a pallet ships |
| `crash-<container>` for the 14 stations and `bt` | PASS | death certificate after 0.3 to 0.4 s (bound 15 s), container back after 0.9 to 1.1 s (bound 60 s), all 60 pans kept, flow resumed after 24 to 29 s; the depositing station needed 94 s (an earlier run; the 15-run figures for it are in `experiment.md`) |
| `freeze-thm-bake` | PASS | broker declared the station dead after 7.1 s (bound 15 s), flow resumed after 21 s |
| `band-halt` | PASS | S1 and S2 halted after 7.3 s, S3 to S5 kept running, all sections ran again when the station returned |
| `band-section-stop` | PASS | the stopped section is "fault", upstream halted, downstream runs, depositing really stops, the repair restarts the sections in order (4.6 s) |
| `bt-down` | PASS | the sections show as unknown at once, they run again when BT is back |
| `mon-down` | PASS | 4 of 4 discard events arrived after MON came back |
| `sql-down` | PASS | 30 s without SQL Server: all nodes online, no band section halted, flow resumed after 25 s |
| `broker-restart` | PASS | all nodes online again after 4.5 s, 60 of 60 pans kept, flow resumed after 25 s |
| `cooling-chain` | PASS | FC15 after 1 s, buffer full (FC18, 30 of 30) after 25 s, first cake discarded (FC17) 15 s later, repair released the buffer (FC16) |
| `faults-system` | PASS | every failure case produced its event, all 60 pans kept |
| `faults-maintenance` | PASS | every case: error event, ticket opened, repair confirmed, recovered within 2 to 18 s |

## How the checks work (so you can extend them)

`cmd/scenario/main.go` talks to the dashboard's API and to `docker compose`:

- `POST /api/inject {node, code, count}` arms a failure case in a node;
- `GET /api/events?since=<id>` returns the events that MON stored after an id;
- `GET /api/state` returns nodes, tickets and alerts;
- `POST /api/repair {ticket}` is the Maintenance Department's *Mark repaired*;
- `GET /api/pans/summary` counts the pans in the loop.

A new failure case is one row in the `systemFaults` or `maintenanceFaults` table
in that file: node, code, the event to expect, and the machine id of the ticket if
Maintenance is needed.
