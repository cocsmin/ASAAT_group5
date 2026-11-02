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

**Decision: one Docker container per system (subsystem), not per component or
device.** Docker, not Kubernetes.

**Why.** The report's availability approach is to separate each system into a
service in a container (`docker-compose.yml`). Five application containers plus
three infrastructure containers is easy to run and explain. A container per
sensor or machine would multiply the number of processes without adding
isolation that matters here: the unit that must fail, restart and deploy
independently is the system (see the "24/7 / Deployability" columns of the
report), and inside a system the machines are modelled as Sparkplug *devices*
with their own health state. Docker's `restart: unless-stopped` is the recovery
tactic of FC27, so Kubernetes would be overkill.

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
| Who receives "Band state change" | THM and FIN, with the state of all five sections. | `internal/mon` |
| Target weight and tolerance to FIN's weight check | Carried in the order signal (`fillTarget`, `fillTol`). | `internal/plm` |
| "Repair confirmed" | Sent by `mon` when the Maintenance ticket is closed on the dashboard; carries `machineId`. | `internal/mon` |
| Ingredient result for FC2 | `staged`: BAT continues with ingredients already at the batching station. | `internal/plm`, `internal/bat` |
| Band section stop (FC19) | The stopped section halts all upstream sections; downstream sections keep running. After repair, upstream sections restart one by one. | `internal/mon` |


## 6. Conformance with the diagrams and rules

Sources read: the Papyrus state machines on `state-machines-fin` (THM SM1 to SM5,
FIN SM1 to SM5), the BAT state machines (Dosing, Mixing and Handoff, Reporting;
commit `c003a9e` on `requirement-diagrams-BAT`), the use cases and failure cases
of the report, the failure-case rules, and the lecture 6 draft ADRs. There is no
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
| FIN restart must not lose cakes or pans | FIN rebuilds its queues from the stored pan and cake documents (`fin.recover`); after depanning the cake has its own document; THM restores the pans FIN had supplied (`thm.restoreSupplied`). Tested by killing FIN, PLM and MON under load: 20 of 20 pans still accounted for, none duplicated |
| PLM restart must not hang BAT | BAT repeats the ingredient request after 20 s; PLM answers a repeated request idempotently (SQL reservation check) |
| Lecture ADR-2 configuration: restart policy, healthchecks, start order, volume per service, non-root, read-only root, limits | `docker-compose.yml` |

### Known gaps (not implemented, or deliberately different)

- **Container granularity.** One container per system. The lecture draft ADR-2
  proposes a hybrid: critical functions alone (for example a separate
  `thm-baking-zone-control` for THM-REQ-74) and Health Reporting separate from
  what it monitors. This needs a group decision. Splitting later does not change
  the topics, because machines are already Sparkplug devices.
- **Different from the lecture drafts, on the group's decision:** Sparkplug B with
  Protocol Buffers instead of MQTT 5 with JSON (ADR-1); SQL Server plus MongoDB
  instead of SQLite plus PostgreSQL (ADR-4), so the event outbox is a file, not a
  SQLite table.
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
