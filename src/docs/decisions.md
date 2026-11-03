# Architecture decisions

Status of each decision as implemented in this repository.

## 1. Sparkplug B over MQTT for health, status and commands

**Decision.** All systems talk through an MQTT broker using Sparkplug B topics
and Protocol Buffers payloads.

**Why.** A broker-based publish/subscribe model suits a central health monitor.
Birth and death certificates and the Last Will give built-in presence detection.
The defined topic structure and payload give shared semantics for status,
alerts and commands. It is light enough for constrained devices and simple
enough to carry both health signals and basic logging on one technology, which
keeps complexity low.

**How it is used.** See "Sparkplug B usage" in the [README](../README.md). The
simple logger is the single event signal ("Log, warning, error and discard
events") plus the SQL event log in `mon`.

**Deviations from the Sparkplug specification, on purpose.**
- Events and commands use QoS 1, because traceability has regulatory relevance
  and a lost repair confirmation would leave a machine locked out. Everything
  else is QoS 0 as the specification says.
- Systems consume each other's `NDATA` directly (peer to peer) instead of
  everything going through the host application, so the signal names of the
  alignment baseline stay exactly as they are.
- State signals ("Depositing stopped / working again", "Stop / resume pan
  admission", "Band state change") are repeated with every heartbeat instead of
  being MQTT retained messages, because one Sparkplug `NDATA` topic per node
  cannot hold several retained values. A restarted receiver learns the current
  value within one heartbeat interval (THM-REQ-25).
- The Sparkplug `STATE` topic of the host (MON) makes the rule "production may
  not run without the Monitor operating" (UC2) real: without it THM admits no
  new pans, BAT starts no new batch and PLM releases no order.

## 2. Databases

**SQL (Microsoft SQL Server) for the main system.** Recipes, ingredient stock,
reservations, orders, shipments, the event log and maintenance tickets. The data
is relational and needs joins and transactions (a reservation must update stock
atomically), so SQL is sufficient and NoSQL is not needed here. Schema:
`internal/store/sql_mssql.go`.

**NoSQL (MongoDB) for pans and node state.** A simulated pan is a document that
is passed from one station to another: each station appends to its history and
status. Node state snapshots live there too so a restarted container reloads
them (FC27). Without Docker the same interface stores pans as JSON files.

## 3. Containerization

**Decided:** Docker (not Kubernetes), and **one container per station** (group
decision, 3 Nov): 14 stations, Band Transport and the monitor, plus the broker and the
two databases; see [stations.md](stations.md).

**Why.** The report's availability approach is to separate the parts of the line into
services in containers (`docker-compose.yml`). A station is the smallest unit that
fails, restarts and deploys on its own with a visible effect on the line: a crashed
cooling does not stop the baking tunnel. A container per sensor or machine would
multiply the processes without adding isolation that matters here; inside a station the
machines are modelled as Sparkplug *devices* with their own health state. Docker's
`restart: unless-stopped` is the recovery tactic of FC27, so Kubernetes would be
overkill.

**Revisit if.** Real hardware connects through gateways that must be deployed
separately. A gateway container per physical cell could be added without
changing the Sparkplug topic structure, because devices are already separate.

## 4. Language: Go for everything

All services, the dev tooling and the dashboard server are Go. The dashboard
itself is one static HTML file with no build step.

## 5. Choices made while implementing

These close open items of the alignment baseline. Change them in one place each.

| Open item | Choice | Where |
|---|---|---|
| Who creates the batch ID | PLM creates it with the production order (`B0001`); a re-started batch gets a suffix (`B0001-2`). It travels in every signal and pan document. | `internal/plm` |
| Heartbeat interval | One value, `HEARTBEAT_SECONDS`, default 5 s. The monitor declares a node silent after three missed beats (so it is above the fault report latency P5). | `internal/svc`, `internal/mon` |
| Who sends and receives "Band state change" | **Band Transport (BT)** sends it to THM, FIN and MON, with the state of all five sections (the monitor additionally gets the detail "fault"). MON no longer owns the band; it sends BT the "Section command" (halt, slow, run) and "Target speed". | `internal/bt`, `internal/mon` |
| Target weight and tolerance to FIN's weight check | Carried in the order signal (`fillTarget`, `fillTol`). | `internal/plm` |
| "Repair confirmed" | Sent by `mon` when the Maintenance ticket is closed on the dashboard; carries `machineId`. | `internal/mon` |
| Ingredient result for FC2 | `staged`: BAT continues with ingredients already at the batching station. | `internal/plm`, `internal/bat` |
| Band section stop (FC19) | The stopped section halts all upstream sections; downstream sections keep running. After repair, upstream sections restart one by one. | `internal/bt` |


## 6. Conformance with the diagrams and rules

Sources read: the Papyrus state machines on `state-machines-fin` (THM SM1 to SM5,
FIN SM1 to SM5), the BAT state machines (Dosing, Mixing and Handoff, Reporting;
commit `c003a9e` on `requirement-diagrams-BAT`), the use cases and failure cases
of the report, the failure-case rules. There is no
PLM or MON state machine yet.

| Rule or diagram | Implemented as |
|---|---|
| THM SM1: pan check, fill weight, N_wrong lockout, admission stop, resume only when no stop reason is active | `thm.setReason` keeps a set of stop reasons; pans are admitted only when it is empty |
| THM SM2: zone adjusts, faulty after T_adjust, zone control keeps regulating during band changes | `thm.zoneLoop`; a band halt never stops the zone loop |
| THM SM3: camera failure diverts all cakes; buffer full diverts all cakes until it has free places | `thm.exitCheck` |
| THM SM4: increased cooling, cooling failed; repaired within 30 min releases buffered cakes in arrival order and measures each; later than 30 min sends them to refuse | `thm.coolingRepaired`, `thm.releaseLoop` (30 min = 40 simulated seconds) |
| THM SM5 and FIN SM5: Band state change; halt = accept nothing new and finish product inside; slow-down; restart | `thm.onBand`, FIN band handler and `fin.depanner` |
| THM-REQ-14, -15: fill fewer pans when stock is low | `thm.depositOne` slows filling at stock <= 2 |
| FIN SM1: only cakes released at a safe temperature are accepted; cakes are held while depanning is down | FIN Cooled cake handler, `fin.depanner` |
| FIN SM2: wrap and carton checks, refuse bin, a full holding area stops pan admission | `fin.wrapper` (UC14 injection), `fin.pushToWrap` |
| FIN SM4: washing station faulted keeps supplying from stock; after repair catch-up speed until backlog = 0, both changes logged | `fin.washer` |
| BAT Dosing: reserve, dispense, verify, top up, discard, lock after P2 failures | `bat.dose`, `bat.lockout` |
| BAT Mixing and Handoff: hold while depositing is stopped, re-mix, mixer fault, discard aged batter after P3 | `bat.process`, `bat.mix`, `bat.handOff` |
| THM SM1 T1 guard: Weight-Checker calibrated | `thm.depositOne` (UC5 injection: depositing waits for recalibration) |
| BAT Dosing: Reserving for every batch | `bat.reserve` runs again for a batch that replaces a discarded one |
| UC2 error flow: halt intent to the band, halt reported to the overseer | `mon.reportHalt` |
| FC27 restart: cakes on the band are not forgotten | `thm.resumeInFlight`, `thm.restoreBuffered`, `fin.recover` |
| Rule: repeated failure of the same check calls Maintenance | BAT (P2), THM (N_wrong), FIN inspection (3 in a row) |
| Rule: the baking tunnel cannot go backwards, pans inside finish | a zone fault stops admission only |
| Rule: cooling buffer on a separate level, cakes that waited too long are discarded | `thm.buffer`, `thm.releaseLoop` |
| UC2: production may not run without the Monitor | host `STATE` topic, see section 1 |
| THM-REQ-72: stopped controller detected within 3 heartbeats | MON watchdog, plus NDEATH (Last Will) after 1.5 x the 5 s keep-alive |
| THM-REQ-77: no lost discard events | event outbox (file, QoS 1, removed after the broker acknowledged, survives a sender restart) **and** a persistent MQTT session for MON, so the broker queues events while MON is down. Tested: 5 discards emitted while MON was down all reached MON's log after it restarted. MON drops duplicates by event id |
| Handoffs between systems survive a restart of the receiver | persistent sessions plus QoS 1 and a message id on Batter ready, Cooled cake, Pan for washing, Pallet ready, Clean pan supply, Pan request, both ingredient signals and the production order; receivers drop duplicates; state signals are QoS 1 and repeated every heartbeat |
| FIN restart must not lose cakes or pans | FIN rebuilds its queues from the stored pan and cake documents (`fin.recover`); after depanning the cake has its own document; THM restores the pans FIN had supplied (`thm.restoreSupplied`). Tested by killing FIN, PLM and MON under load: all pans still accounted for, none duplicated |
| PLM restart must not hang BAT | BAT repeats the ingredient request after 20 s; PLM answers a repeated request idempotently (SQL reservation check) |
| Container configuration: restart policy, healthchecks, start order, volume per service, non-root, read-only root, limits | `docker-compose.yml` |

### Known gaps (not implemented, or deliberately different)

- **Event outbox:** because the databases are SQL Server and MongoDB and the
  control path must not depend on them, each system keeps its undelivered events in a
  local file, not in a database table.
- **BAT handoff.** The 14 signals have no "batter collected" acknowledgement, so
  batter only ages while depositing is stopped.
- **THM restart.** Cakes in the tunnel and the cooling zone are taken up again and
  the cooling buffer is restored (`thm.resumeInFlight`, `thm.restoreBuffered`).
  A resumed pan bakes from zone 1 again, and a cake that was being deposited at
  the crash is not resumed.
- **MON** halts band sections for FC19, FC20 and for silent or dead nodes and
  reports the halt or slow-down as in the UC2 error flow. A general policy table
  for any error waits for the MON document.
- **Band state** carries the state of all five sections; each receiver derives
  halt, slow-down or run from the sections it owns.
- Beyond the 14 signals of the baseline there are two operations messages:
  `Fault injection` (demo) and `Node Control` (Sparkplug rebirth).
- **Delivery guarantee.** At least once with deduplication, not exactly once. The
  duplicate filter is in memory, so a duplicate that arrives after the receiver
  restarted is handled by the checks in the receivers (batch id, pan id), not by
  the filter.
- **Docker:** `docker compose up --build` was run with the real SQL Server and
  MongoDB; see TODO.md for what was measured. The CI pipeline
  (`.github/workflows/cicd.yml`) is new and has not run on GitHub yet.
- **Component control.** The dashboard switches parts on and off through a
  control service: the `ctl` container in Docker (opt-in profile `demo`, it holds
  the Docker socket) or the dev launcher. The monitor cannot switch itself off.
- **Lost orders.** An order that batter preparation received but had not yet started
  is lost when it crashes. PLM sends an order again after 40 s without an ingredient
  request, and BAT ignores an order id it already has. A batch lost in the middle
  of dosing stays lost (a fault costs at most one batch, BAT-NFR-02).
- **Inbound messages.** The MQTT client callback only queues a message; a worker
  processes it. A blocking handler would otherwise stop the keep-alive and the node
  would fall off the broker for good.
- **Database outages.** The monitor's health path does not touch the database: its
  event and ticket writes are queued in the background. Document store calls time
  out after 3 s.
- **Security** (TLS, broker access control, login) is not a stated requirement and
  is not implemented.

Everything still open, including the diagrams that need fixing, is in [../TODO.md](../TODO.md). The signals as implemented are in [signals.md](signals.md).

## 7. Decisions of the group (log)

Decisions the group has taken, so that the review list ([team-review.md](team-review.md))
only holds what is still open. Newest last.

**3 Nov**
- The `mongo-down` scenario is dropped: MongoDB holds experiment data and the pan
  documents, an outage does not stop production.
- The implementation decides the time values and checks them for consistency. Result:
  seconds instead of plant time (the report gives no real timings); UC10's 30 minutes are
  40 s (a fixed factor of 45); T_hb is 5 s for every system; both the 15 s watchdog
  (THM-REQ-72, the requirement) and the 5 s MQTT keep-alive (an extra) stay; THM-REQ-76 and
  -78 (1 s) are not measured; availability counts the time the controller does not answer,
  not the time until the line produces again. Shared values are in
  `internal/timing/timing.go`, checked by `timing_test.go`.
- PLM sends a heartbeat like every other system (the alignment baseline says it sends none:
  the baseline has to be corrected).
- The line should be full: 30 pans per batch, 60 pans in the loop, pallet 30 cakes.
- One container per station instead of one per system (see [stations.md](stations.md)).
- One branch, `implementation`; the CI pipeline is checked once everything is finished and
  a pull request exists.
- Realistic values for the simulation are chosen by the implementation and listed in
  [assumptions.md](assumptions.md); the group may change any number.
- Dev mode (`go run ./cmd/dev`) is removed: Docker is the only way to run the factory.
- A batch that is lost when a BAT station restarts is discarded (food safety: nothing of
  unknown age is used) and a new one is made.
- Unit tests run in CI and a failing test fails the pull request (to block merging, the
  check must be marked required in the repository settings).
- 24 pans per batch, 48 pans in the loop, pallet and buffer of 24 (see assumptions.md
  section 6); the cooling limit is 28 C.
- The case of several stations failing at once is covered by two scenarios
  (`freeze-two-stations`, `freeze-three-stations`).
