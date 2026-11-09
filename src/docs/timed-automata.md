# Exercise 8, D3: Semantic anchoring in timed automata

Draft for the report. The models are in UPPAAL 5.0 and were checked with `verifyta`: see
[uppaal/](uppaal/). A beginner's guide to the diagrams (no prior knowledge needed) is
[uppaal/README.md](uppaal/README.md). The tables below describe the same automata in words. Clocks are in
seconds. All timing values that the plant does not give are estimated by the group
(restart time, message delay, how long depositing may stay stopped). `x`, `y`, `z` are local clocks. `!` sends and `?` receives on a channel.

Time constants (`internal/timing`): `HB = 5`, `KA = 5` (the broker declares a node dead after 1.5 x 5 s = 7.5 s, rounded up to `DEADAFTER = 8`), `DETECT = 15`, `RESTORE = 60`,
`ESCALATE = 45`, `P3 = 25`, `LOST = 120`, `GRACE = 3`, `RECONCILE = 5`.

## D3.1 Intra-behaviour: one TA per function block

### Node (any station)

Channels: `hb!`, `birth!`, `crash?` and `freeze?` (from the environment), `restart?` (from Docker).
`hb!` stands for the node's regular traffic: the status message to MON and the MQTT
keep-alive ping, both every 5 s in the code (`timing.Heartbeat`, `timing.KeepAlive`).

| Location | Invariant | Meaning |
|---|---|---|
| `Starting` | `x <= 1` | boots and connects |
| `Online` | `x <= HB` | publishes a heartbeat every `HB` |
| `Down` | none | crashed or frozen, silent |

| From | To | Guard | Sync | Update |
|---|---|---|---|---|
| `Starting` | `Online` | | `birth!` | |
| `Online` | `Online` | `x >= HB` | `hb!` | `x = 0` |
| `Online` | `Down` | | `crash?` | |
| `Online` | `Down` | | `freeze?` | |
| `Down` | `Starting` | | `restart?` | `x = 0` |

### Broker (death certificate)

Channels: `hb?` (any node traffic), `crash?` (the connection closes), `ndeath!`.

| Location | Invariant | Meaning |
|---|---|---|
| `Alive` | `y <= DEADAFTER` | the node's keep-alive is running |
| `Notify` | committed | the connection closed, the Last Will is sent at once |
| `Dead` | none | |

| From | To | Guard | Sync | Update |
|---|---|---|---|---|
| `Alive` | `Alive` | | `hb?` | `y = 0` |
| `Alive` | `Alive` | | `birth?` | `y = 0` |
| `Alive` | `Notify` | | `crash?` | |
| `Notify` | `Dead` | | `ndeath!` | |
| `Alive` | `Dead` | `y >= DEADAFTER` | `ndeath!` | |
| `Dead` | `Alive` | | `birth?` | `y = 0` |

The invariant `y <= DEADAFTER` forces the broker to declare the node dead after 1.5 x the 5 s
keep-alive = 7.5 s without any traffic (rounded up to 8 in the model). A crash is faster: the Last
Will fires when the connection closes, so the broker goes through the committed `Notify` and sends
`ndeath!` as soon as `crash` happens.

### MON (supervisor)

Channels: `ndeath?`, `birth?`, `escalate!`. The model has only the death-certificate path. The
code has a second one, the watchdog of MON (3 missed heartbeats = 15 s, FC28), which is slower and
so does not change the proven bound.

| Location | Invariant | Meaning |
|---|---|---|
| `Watching` | none | all nodes known online |
| `Failed` | `z <= ESCALATE` | a node is dead, waiting for its restart |
| `Escalated` | none | ticket for Maintenance |

| From | To | Guard | Sync | Update |
|---|---|---|---|---|
| `Watching` | `Failed` | | `ndeath?` | `z = 0` |
| `Failed` | `Watching` | | `birth?` | |
| `Failed` | `Escalated` | `z >= ESCALATE` | `escalate!` | |

`Escalated` has no way out in the model (the repair by Maintenance is not modelled; the query
`A[] not MON.Escalated` says it is never reached).

### Band section (one TA per section S1 to S5)

Channels: `halt[i]?` (the owner of section i failed), `stop[i]?` and `stop[i-1]!` (the halt
passes to the section upstream; `stop` is a broadcast so the most upstream section can send
to a sink), `release?`.

| Location | Invariant | Meaning |
|---|---|---|
| `Running` | none | |
| `Pass` | `x <= 1` | the halt must be passed on within 1 s (T-RC3) |
| `Stopped` | none | stopped and upstream told |

| From | To | Guard | Sync | Update |
|---|---|---|---|---|
| `Running` | `Pass` | | `halt[i]?` (own halt) | `x = 0` |
| `Running` | `Pass` | | `stop[i]?` (halt from downstream) | `x = 0` |
| `Pass` | `Stopped` | | `stop[i-1]!` | `nstopped++` |
| `Stopped` | `Running` | | `release?` | `nstopped--` |

### Batter handoff (BAT-INSP, THM-DEP and BAT-INSP's copy of the depositing state)

This follows the implementation and the decision in `team-review.md` (section 1, item 2,
option B): there is **no acknowledgement** from depositing to batter preparation. BAT-INSP
reads its own copy of the state signal "Depositing stopped / working again". While the copy
says stopped it holds the batter; after `P3` (plus one polling second) it discards it.
Otherwise it sends "Batter ready" and is done with the batch.

Constants: `MIX = 6`, `INSP = 3`, `P3 = 25`, `POLL = 1` (the code looks at the state once a
second, so it discards after 26 s), `LOST = 120`, `STALE = 6` (the copy is at most one
heartbeat plus one delivery old, estimated), `BLOCKMAX = 60` (depositing stays stopped at
most this long, estimated), `DEPSTART = 24` (a queued batter waits at most this long for
depositing to take it up: it can sit behind the previous batch, 24 pans of 1 s each, from the code). Clocks: `g` age of the batch, `h` time since approval.
Channel: `ready` (Batter ready).

**Batch** (BAT-INSP):

| Location | Invariant | Meaning |
|---|---|---|
| `Mixing` | `x <= MIX` | mixing |
| `Inspecting` | `x <= INSP` | inspection takes 3 s |
| `Holding` | `x <= P3 + POLL` | the copy says depositing is stopped, the batter waits |
| `Handed` | none | "Batter ready" sent, BAT is done |
| `Discarded` | none | waited too long |

| From | To | Guard | Sync | Update |
|---|---|---|---|---|
| `Mixing` | `Inspecting` | `x >= MIX` | | `x = 0` |
| `Inspecting` | `Holding` | `x >= INSP && seen` | | `x = 0, h = 0` |
| `Inspecting` | `Handed` | `x >= INSP && !seen` | `ready!` | `h = 0` |
| `Holding` | `Handed` | `!seen` | `ready!` | |
| `Holding` | `Discarded` | `seen && x >= P3 + POLL` | | |

**Dep** (THM-DEP): `Open`, `OpenQ` (a batter is queued, invariant `q <= DEPSTART`),
`Blocked` (invariant `y <= BLOCKMAX`), `Took` (committed: the batter is taken up), `Done`.
It accepts `ready?` in `Open` and `Blocked` (it queues the batter whatever its state). It
can stop once (`stopped = true`) and starts again after at least 1 s. `OpenQ` to `Took` sets
`deposited`. **Copy**: one location with invariant `w <= STALE`; every 1 to 6 s it sets
`seen = stopped`. This is the state signal, repeated with every heartbeat.

### Station handover (sender and receiver)

Sender: `Idle`, `Stored` (document written), `Signalled`. Edge `Stored` to `Signalled`
sends `handoff!` (the message). Receiver: `Idle`, `Adopted`. Edges: `handoff?` to
`Adopted`; or, when the message is lost, after `GRACE` the reconcile pass takes the document
(`x >= GRACE` and a pass every `RECONCILE`), invariant `x <= GRACE + RECONCILE`.

### PLM-WH (the order and warehouse state machine)

The code is the state machine of the Papyrus model (`internal/plm/machine.go`, PLM StateMachine22),
one goroutine handling one event at a time. Constants: `ORDER = 22`, `WITHDRAW = 3` (the ingredients are
collected), `RESTOCK = 15` (Logistics restocks), `BREAKER = 15`, `WTO = 2` (warehouse timeout),
`RETRY = 20` (BAT repeats an unanswered request), `MAXOUT = 2`, `DOWNMAX = 40` (the warehouse stays
silent at most this long, estimated). Channels: `req` (urgent broadcast, BAT to PLM), `confirmed`,
`insufficient`, `staged` (broadcast, PLM to BAT).

**Plm**:

| Location | Invariant | Meaning (state of the diagram) |
|---|---|---|
| `Idle` | none | Idle: waits for an ingredient request or for the order cycle |
| `Back` | committed | entry of Idle: a due order is created (Scheduling_Order, Fetching_recipe_targets) or dropped |
| `Verifying` | committed | Verifying_Inventory and the StockCheck choice |
| `TimingOut` | `t <= WTO` | the warehouse does not answer (wms_timeout is coming) |
| `Waiting` | `w <= WITHDRAW` | Reserving_Ingredients, then Waiting_For_Withdrawal |
| `Holding` | `h <= RESTOCK` | Holding_Order |
| `HoldAns` | committed | answers a repeated request while holding |
| `Suspended` | `b <= BREAKER` | Communication_Suspended |
| `SusAns` | committed | answers a request with "staged" while suspended |

| From | To | Guard | Sync | Update |
|---|---|---|---|---|
| `Idle` | `Back` | `due` | | |
| `Back` | `Idle` | `due && orders < MAXOUT` | | `due = false, orders++, made++` (the order is created and sent) |
| `Back` | `Idle` | `!(due && orders < MAXOUT)` | | `due = false` (nothing due, or BAT has two orders: the cycle is dropped) |
| `Idle` | `Verifying` | | `req?` | |
| `Verifying` | `TimingOut` | `wmsDown` | | `t = 0` |
| `Verifying` | `Holding` | `!wmsDown && lowStock` | `insufficient!` | `h = 0` |
| `Verifying` | `Waiting` | `!wmsDown && !lowStock` | | `w = 0` |
| `TimingOut` | `Suspended` | `t >= WTO` | `staged!` | `b = 0` |
| `Waiting` | `Back` | `w >= WITHDRAW` | `confirmed!` | `withdrawn = true` |
| `Waiting` | `Waiting` | | `req?` | (a repeated request: the answer is on its way) |
| `Holding` | `Verifying` | `h >= RESTOCK` | | `lowStock = false` |
| `Holding` | `HoldAns`, back | | `req?`, then `insufficient!` | |
| `Suspended` | `Back` | `b >= BREAKER && !wmsDown` | | (the test request succeeds) |
| `Suspended` | `Suspended` | `b >= BREAKER && wmsDown` | | `b = 0` (test again later) |
| `Suspended` | `SusAns`, back | | `req?`, then `staged!` | |

**Bat** (BAT-DOSE asking for ingredients): `Ready`, `Waiting` (`c <= RETRY`), `Retry` (committed). It
sends `req!` for an order it holds, waits for `confirmed`, `staged` or `insufficient` (it then keeps
waiting), and repeats the request after `RETRY`. **Clock** sets `due` every `ORDER`. **Stock** makes
the stock too low once (FC1). **Wms** makes the warehouse silent once, for at most `DOWNMAX` (FC2).

## D3.2 Inter-behaviour: networks of timed automata

NTA 1, supervision: `Node(i)` x N, `Broker(i)` x N, `MON`, `Docker`, `Env`.

| Channel | From | To | Kind |
|---|---|---|---|
| `crash[i]` | Env | Node i, Broker i, Docker | broadcast (committed edge in the broker) |
| `freeze[i]` | Env | Node i, Docker | broadcast |
| `restart[i]` | Docker | Node i | binary |
| `hb[i]` | Node i | Broker i | binary |
| `birth[i]` | Node i | Broker i, MON | broadcast |
| `ndeath[i]` | Broker i | MON | broadcast |
| `escalate`, `repaired` | MON, Env | | binary |

Docker is a small TA: `Idle` to `Restarting` when it hears `crash` or `freeze` (the fault itself,
not MON and not the death certificate), then after a delay `d` with `1 <= d <= 30` (assumed; the
experiment measured at most 1.2 s for a crash) `restart[i]!`. This follows the code for a crash: the
restart policy of Docker (`restart: unless-stopped`) restarts the container when the process exits,
without waiting for MON. For a freeze (`docker pause`) Docker does nothing; the scenario unpauses the
container. So for a freeze the delay is an assumption about how long the pause lasts, not a property
of the code. The broker also counts a `birth` as traffic (it resets its keep-alive clock `y`), so a
node that returns before the broker has declared it dead is not declared dead afterwards.

NTA 2, band: `Section(1..5)`, `Station(i)`, `MON`. Channels: `halt[s]`, `release[s]`,
`stop_upstream[s]`. A halt of section s is forwarded to every section below s (one
`stop_upstream` each); MON sends `release[s]` only after `birth` of the owner (T-OR2).

NTA 3, batter: `Batch`, `Dep`, `Copy` (the state copy). `Batch` and `Dep` meet only on `ready`;
`Batch` sees `Dep` only through the delayed copy `seen`. The 120 s limit of dosing is checked as
a query on the clock `g` (the dosing timer itself is not modelled).

NTA 4, handover: `Sender`, `Receiver`, a lossy `Message` automaton that either delivers or
drops.

NTA 5, PLM-WH: `Plm`, `Bat`, `Clock`, `Stock`, `Wms`. `Bat` and `Plm` meet on `req` and on the three
answers; `Stock` and `Wms` only change `lowStock` and `wmsDown`, and `Clock` only sets `due`.

## D3.3 How the timing constraints enter the automata

| Constraint | Where in the TA | How |
|---|---|---|
| T-P1 heartbeat 5 s | Node, edge `Online` to `Online` | invariant `x <= HB` and guard `x >= HB` |
| T-RC1 detection 15 s | Broker invariant, MON | proven by the query `A[] MON.Failed imply ...` below |
| T-RC2 recovery 60 s | Node, Docker, MON | `ESCALATE` guard on MON; query bounds restart plus birth |
| T-RC3 stop within 1 s | Band section | invariant `x <= 1` in `Pass` |
| T-RC4 batter 25 s | Batch | invariant `x <= P3 + POLL` in `Holding` and guard `x >= P3 + POLL` on the discard; covers the wait **before** the handoff only (see Limits) |
| T-RC6 batch lost 120 s | queries on the clock `g` | `g <= LOST` while the batch is with BAT and when the batter is deposited; the dosing timer itself is not modelled |
| T-RC7 handover 3 s | Handover | guard `x >= GRACE`, invariant `x <= GRACE + RECONCILE` |
| T-EX* execution times | Station TAs | invariant `x <= d` and guard `x >= d` |
| T-OR2 release after birth | Band NTA | `release` edge guarded by `owner_online` |
| T-P5 order cycle 22 s | PLM NTA, `Clock` | invariant `c <= ORDER`, guard `c >= ORDER`; at most one order per cycle (`made <= 1`) |
| T-D3 warehouse timeout 2 s, breaker 15 s | PLM NTA, `TimingOut`, `Suspended` | invariants `t <= WTO`, `b <= BREAKER` |
| T-D4 ingredient request repeated after 20 s | PLM NTA, `Bat` | invariant `c <= RETRY` in `Waiting` |
| T-D5 withdrawal 3 s, T-D6 restock 15 s | PLM NTA, `Plm` | invariants `w <= WITHDRAW` in `Waiting`, `h <= RESTOCK` in `Holding` |

Each constraint is an invariant (it forces the system to move on) or a guard (it holds
the move back). Reaction constraints that span several automata (T-RC1, T-RC2) are not
written into one automaton. They are properties of the NTA, checked with a query.

### Machine-checked models

| File | Model | Queries | Result |
|---|---|---|---|
| `uppaal/supervision.xml` | Node, Broker, MON, Docker, Env | 8 | all satisfied |
| `uppaal/band.xml` | five Sections, Env, MON | 7 | all satisfied |
| `uppaal/batter.xml` | Batch, Dep, Copy | 11 | all satisfied (two of them are reachability findings, see below) |
| `uppaal/handover.xml` | Sender, Message, Receiver | 4 | all satisfied |
| `uppaal/plm.xml` | Plm, Bat, Clock, Stock, Wms | 13 | all satisfied |

Run (from `src/docs/uppaal`): `node gen.js` writes the models and `.q` files,
`verifyta -q supervision.xml supervision.q` checks them. `gen.js` is the source; edit it, not the XML.

What the queries prove, with the estimated parameters:

- **Supervision.** A silent node is known to MON within 8 s (`DEADAFTER`), inside the 15 s
  limit. A node is back within 60 s (a restart takes at most 30 s, estimated). MON never
  escalates (45 s). Every failure ends in recovery (leads-to). No deadlock.
- **Band.** A halt reaches every upstream section within N x 1 s = 5 s, each section hands it on
  within 1 s. A section is released only after its owner is back. After the release no section
  stays stopped.
- **Batter.** Before the handoff the batter holds at most P3 + POLL = 26 s. BAT's side of a batch
  is finished within 6 + 3 + 26 = 35 s, far inside the 120 s limit. A batter is always deposited
  before dosing gives the batch up (`g <= LOST`, with the estimated `BLOCKMAX`). Deposited and
  discarded exclude each other. Both end states are reachable.
- **Batter, two findings** (queries that are satisfied and show a weakness, not a proof of
  safety). (1) The state copy can be stale: a batter can be handed to depositing while
  depositing is already stopped (`E<> Batch.Handed && stopped`). Depositing queues it, so
  nothing is lost. (2) After the handoff nothing in the code limits the wait: a batter can be
  deposited more than `P3 + POLL` after approval, at most `DEPSTART + BLOCKMAX + DEPSTART` = 108 s
  with the estimates. `P3` is checked in BAT-INSP only; THM-DEP does not discard queued batter
  (`internal/thm`). The proposed fix, with no new signal, is in `team-review.md` section 3.
- **Handover.** A stored document is taken up within GRACE + RECONCILE = 8 s, also when the
  message is lost.
- **PLM-WH.** BAT has at most two orders it has not asked ingredients for; at most one order is
  created per order cycle (a cycle that ends while the machine is busy is taken up when it is Idle
  again, never more); BAT is confirmed only after a withdrawal was booked; BAT gets its final answer
  ("confirmed" or "staged") within RESTOCK + WITHDRAW = 18 s, which is below the 20 s after which it
  repeats the request (the bound is tight: 17 s fails); a held order is always resolved; a suspension
  ends within BREAKER + WTO = 17 s after the warehouse answers again; no deadlock.

**Negative controls** (to show the queries can fail): with a restart time of up to 60 s the
60 s recovery query and the no-escalation query fail; with a batch limit of 30 s instead of
120 s the two LOST queries of the batter model fail; with depositing blocked for up to 120 s
instead of 60 s the query "a batter is deposited before dosing gives the batch up" fails; in the PLM
model a restock time of 25 s or a withdrawal time of 30 s makes the 18 s answer query fail.

## Limits

- The models are hand-written from the code and documents and are not generated from the
  code, so they can drift. The UPPAAL check is not part of the CI.
- Each model has one node, one fault and one batch; several faults at once are not covered.
- Time is simulated seconds; the supervision values are wall-clock.
- The restart delay (1 to 30 s), message delay (1 s), and the time depositing may stay stopped
  (60 s) are estimates. The measured restart is at most 1.2 s, so the proven bounds are safe,
  not tight.
- The batter model has no acknowledgement from depositing, as in the code. If the group
  chooses option A of `team-review.md` (a 15th signal "Batter collected"), `Batch` waits for
  that signal instead of reading `seen`, and the model has to be changed. The race to watch
  then is the acknowledgement in flight while BAT discards at `P3`.
- The PLM model has one BAT that asks for one order at a time, anonymous orders (no ids), one stock
  shortage and one warehouse silence. A restart of PLM-WH is not modelled: the reservation is in
  the database, BAT repeats its request and `Reserve` gives the same answer (unit test
  `TestRestartWhileWaitingForTheWithdrawalLosesNothing`). The recipe that cannot be read, the
  resend of an order after 40 s and the monitor-offline gate are covered by unit tests, not by the model.
  The state `Verifying_Inventory` and the StockCheck choice are one atomic database call in the
  code. The model has no `Tick` (it never ends) and the diagram spells the state `Veryfing_Inventory`.
- The delivery delay of "Batter ready" is not modelled (the signal is QoS 1 with a persistent
  session, so it arrives; a message lost for good is caught by the 120 s timer of dosing).
  BAT-INSP leaves `Holding` at once when the copy says working again; the code looks once a second.
- The band model lets MON release only when every upstream section has confirmed it is
  stopped. That is an assumption about MON, not proven from the code.
