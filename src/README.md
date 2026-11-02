# Cake factory: implementation

A running simulation of the cake production line from the report, built to show
the architecture working: the six systems as services, Sparkplug B over MQTT
between them, and a dashboard that shows the flow of cakes and what happens
when something goes wrong.

## Run it

All commands run in this folder (`src/`).

**With Docker** (the real deployment; Docker Desktop must be running):

```bash
docker compose --profile demo up -d --build
```

Wait about a minute until `docker compose ps` shows every container as `healthy`,
then open the dashboard at <http://localhost:8080>. Stop everything with
`docker compose --profile demo down`.

The `demo` profile adds the small `ctl` container that lets the dashboard switch
the parts on and off (see below). Without it (`docker compose up -d --build`) the
factory and the rest of the dashboard work the same, only the Components card is
off. `ctl` holds the Docker socket, which gives it control over your containers,
so it is opt-in.

**Without Docker** (embedded MQTT broker, SQL data in memory, pan documents as
JSON files under `data/`; needs only Go 1.25+):

```bash
go run ./cmd/dev
```

Open <http://localhost:8080>. Stop with Ctrl+C. Here the Components card works
too (stop, start, restart; freezing needs Docker).

Options (environment variables): `SIM_SPEED=3` runs the factory three times faster
(Docker: `SIM_SPEED=3 docker compose ...`; PowerShell: `$env:SIM_SPEED="3"`),
`HEARTBEAT_SECONDS=2` detects silent nodes sooner, `DEV_RESTART_SECONDS=15` keeps a
crashed service down for 15 s in dev mode, `DEV_KEEP_DATA=1` keeps `data/` between
dev runs (it is wiped at every start otherwise).

## Demonstrate and check

The dashboard has two cards for this, and the same checks run from a terminal:

- **Components**: switch each part off and on (stop, start, freeze, restart) and
  watch the lanes, alerts and event log react: the broker, SQL Server, MongoDB
  and the five systems.
- **Test scenarios**: run a named check (crash a system, freeze a node, restart
  the broker, switch a database off, every failure case) and read a live timeline
  that ends in PASS or FAIL.
- **Inject a failure case**: one button per failure case of the report.
- Terminal: `go run ./cmd/scenario list` and `go run ./cmd/scenario all`.

All 28 failure cases and how to trigger each: [docs/failure-cases.md](docs/failure-cases.md).

See [docs/demo.md](docs/demo.md). The same scenarios run in the CI pipeline.

## What you see

- **Live line**: a picture of the whole factory. Batter, cakes and empty pans move
  along the conveyor while a section runs and stop when it halts. Each station is
  green when healthy, amber when degraded, red (pulsing, with a `!`) when faulty or
  when its container is down, and shows what is wrong. The bars under the line
  are the five band sections. A short red or green message rises from a station
  when a cake is discarded or a pallet ships.

- **Needs attention**: every open fault, with a *Mark repaired* button for the
  Maintenance Department. Green and quiet when the line is healthy.
- **Production flow**: the five systems and their stages with live counts.
  A stage turns amber when degraded and red when faulty. A lane turns red when
  its node is silent or its container died. Below it, the five band sections
  show *running*, *slow*, *halted* or *stopped*.
- **Pans in motion**: the pan documents passed from station to station.
- **Maintenance tickets** and the **event log** (stored in SQL Server).
- **Inject a failure case**: one button per failure case of the report
  (FC1 to FC28, plus the UC4 and UC12 alternatives) so each handling flow can be
  demonstrated on demand.

## Architecture

| Container | System | Owns |
|---|---|---|
| `plm` | Production & Logistics Management | orders, recipes, stock and reservations, shipments (SQL) |
| `bat` | Batter Preparation | dosing, mixing, human inspection gate, batter handoff |
| `thm` | Thermal Processing | depositing, baking zones, tunnel-exit check, cooling and buffer |
| `fin` | Finishing & Packaging | depanning, quality inspection, wrap/carton, pallets, pan washing loop |
| `mon` | Line Monitoring & Health + Band Transport | health detection, event log, tickets, band sections, dashboard |
| `mosquitto`, `mssql`, `mongo` | infrastructure | message broker, relational store, document store |

The decisions (see [docs/decisions.md](docs/decisions.md)): Sparkplug B over
MQTT, SQL Server for the main system, MongoDB for the traveling pans, Docker
for every subsystem, Go for everything.

### Sparkplug B usage

- Topics follow `spBv1.0/CakeFactory/<NBIRTH|NDEATH|NDATA|NCMD|DBIRTH|DDATA>/<node>[/<device>]`.
- Every service is an edge node. Its **Last Will is the NDEATH certificate**, so
  a crashed container is announced by the broker at once (FC27). Each
  `NBIRTH` carries a `bdSeq`, and a changed `bdSeq` tells the monitor the
  container restarted.
- Each machine is a Sparkplug device (`DBIRTH`/`DDATA` with `State` and
  `Detail`), for example `BAT/Doser` or `THM/Zone2`.
- `mon` is the **primary host application**. It publishes a retained `STATE`
  message; nodes republish their births when it comes online. It asks a node
  for a rebirth when it sees data from a node it has no birth for.
- The signals of the alignment baseline keep their exact names and travel as
  metrics named `<Signal>/<field>`, for example `Batter ready/batchId`. The
  payload is hand-encoded Protocol Buffers (`internal/sparkplug`), so no code
  generation is needed.
- Signals between systems (`Batter ready`, `Cooled cake`, ...) are `NDATA`.
  Commands from the monitor (`Repair confirmed`, `Band state change`, fault
  injection) are `NCMD`.
- Log, warning, error and discard events are one signal with a severity field,
  so one messaging technology carries both health signals and logging.

### Where each use case and failure case lives

| Report | Where |
|---|---|
| UC1 retrieve ingredients, FC1, FC2 | `internal/plm` (reservation in SQL, order hold, circuit breaker) |
| UC3, UC4, FC3 to FC7 | `internal/bat` |
| UC5/6, UC9, UC10, FC8 to FC18 | `internal/thm` |
| UC11 to UC16, FC21 to FC26 | `internal/fin` |
| UC2, FC19, FC20, FC27, FC28 | `internal/mon` (policy, band sections, watchdog), `internal/svc` (container recovery) |

Availability tactics from the report: **monitor** (heartbeats, death
certificates, event policy in `mon`), **exception handling** (every failure case
has a handler), **rollback / recovery** (restart policy plus a state snapshot
that a restarted node reloads, the circuit breaker, the cooling buffer that
returns cakes in arrival order).

### Layout

```
cmd/<service>      one main per container (plus devbroker and dev launcher)
internal/sparkplug Sparkplug B payload and topic codec
internal/sig       named signals mapped to metrics
internal/node      edge node: MQTT, LWT, births, heartbeat, events
internal/svc       shared service plumbing: faults, repair, state snapshots
internal/store     MSSQL and MongoDB stores plus in-memory/file fallbacks
internal/{plm,bat,thm,fin,mon}  the five systems
internal/mon/web   the dashboard (static, no build step)
```

## Status and limits

Open work, diagrams to fix and decisions for the group: [TODO.md](TODO.md). The
signals as implemented: [docs/signals.md](docs/signals.md).


- Built and smoke-tested with `go run ./cmd/dev` (all five services, the
  dashboard, fault injection and repair). `go test ./...` covers the Sparkplug
  codec and the signal mapping.
- Run with Docker Desktop: all containers healthy, real SQL Server and MongoDB
  used, a crashed container is restarted and takes up its cakes again (see
  TODO.md). The SQL Server image is x86; on Apple silicon enable Rosetta
  emulation in Docker Desktop.
- The simulation compresses time: the 30 minutes of UC10 are 40 simulated
  seconds; durations are constants at the top of each service file.
- The human inspector of UC4 is simulated (a 3 s delay, rejections via the UC4 injection button).
- Machines are simulated inside each service. Real PLC/OPC UA integration is out
  of scope.
