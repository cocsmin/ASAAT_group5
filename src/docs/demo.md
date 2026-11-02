# Demo and reproducible checks

Everything that goes wrong in the report can be shown live and repeated by anyone.
There are two ways: click the failure case on the dashboard, or run a scenario.
A scenario does something bad to the running factory, prints a timeline of what
the factory does about it, and ends with PASS or FAIL. The same scenarios run in
the CI pipeline.

## From the dashboard

Two cards on the right side of the dashboard do this without a terminal:

- **Components** lists the broker, SQL Server, MongoDB and the five systems with a
  state and buttons: *Stop*, *Start*, *Freeze* (the process stops answering, like
  a hung machine), *Unfreeze*, *Restart*. Each has a line that says what to expect.
  The monitor itself is locked, because switching it off would cut the page you
  are looking at (use the `mon-down` scenario in a terminal).
- **Test scenarios** lists the scenarios below with a *Run* button and a *Run all*
  button. The timeline appears live and ends with PASS or FAIL. Scenarios that
  switch containers need the control service; `mon-down` always runs in a
  terminal.

With Docker the control service is the `ctl` container, started with
`docker compose --profile demo up -d --build`. With `go run ./cmd/dev` it is built
into the launcher (no freezing there).

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
   go run ./cmd/scenario healthy crash-thm
   go run ./cmd/scenario all
   ```

Without Docker, `go run ./cmd/dev` starts the factory and the scenarios that do
not need Docker still work (the ones marked `docker` are skipped).

## The scenarios

| Scenario | What happens to the factory | What it proves |
|---|---|---|
| `healthy` | nothing | all five systems online, a pallet ships |
| `crash-plm`, `crash-bat`, `crash-thm`, `crash-fin` | the container exits like a real crash | the broker publishes the death certificate (Last Will) within 15 s (THM-REQ-72); Docker restarts the container and it is back within 60 s (THM-REQ-73); no pan is lost or duplicated; the line carries on |
| `freeze-thm` | THM freezes and stops its keep-alive pings | the broker declares it dead on its own (about 7.5 s with a 5 s keep-alive), MON halts the band sections; it comes back when unfrozen |
| `mon-down` | MON is stopped while THM discards cakes | no event is lost (THM-REQ-77: the events wait for MON and arrive after it restarts); the line does not run without the monitor (UC2 rule) |
| `sql-down`, `mongo-down` | a database is switched off for 30 s | the monitor does not mistake a database outage for a dead line (no node declared dead, no band section halted), stations carry on, everything catches up when it returns |
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
4. Click `FC19` under MON: a band section stops, upstream sections halt and
   downstream runs empty, the bar below the flow shows it. Repair it and watch the
   sections restart in order.
5. In a terminal: `go run ./cmd/scenario crash-thm`, and say what the timeline
   shows: death certificate after 0.4 s, back after 0.7 s, 20 pans still in the
   loop, flow resumed.
6. Optional: `go run ./cmd/scenario mon-down` to show that no event is lost.

## Measured results

Run on Docker Desktop (Windows 11) with `SIM_SPEED=3`; times are real seconds.

| Scenario | Result | Numbers |
|---|---|---|
| `healthy` | PASS | all nodes online, a pallet ships within 4 s |
| `crash-plm` / `-bat` / `-thm` / `-fin` | PASS | death certificate after 0.4 s, back after 0.7 to 0.8 s (bounds 15 s and 60 s), 20 of 20 pans kept, flow resumed |
| `freeze-thm` | PASS | broker declared THM dead after 5.8 s (bound 15 s), flow resumed |
| `mon-down` | PASS | 4 of 4 discard events arrived after MON came back |
| `sql-down` | PASS | 30 s without SQL Server: all nodes online, no band section halted, flow resumed after 27 s |
| `mongo-down` | PASS | 30 s without MongoDB: all nodes online, no band section halted, flow resumed after 9 s |
| `broker-restart` | PASS | nodes stayed online, 20 of 20 pans kept, flow resumed after 36 s (lost handoffs are repeated by BAT after 20 s) |
| `cooling-chain` | PASS | FC15 after 3 s, buffer full (FC18) after 8 s, first cake discarded (FC17) after 6 s more, repair released the buffer (FC16) |
| `faults-system` | PASS | 16 of 16 failure cases produced their event, 71 s in total, 20 of 20 pans kept |
| `faults-maintenance` | PASS | 13 of 13 (now with FC20 "System, then Maintenance" and the repeated FC24): error event, ticket opened, repair confirmed, recovered; 52 s in total, 20 of 20 pans kept |

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
