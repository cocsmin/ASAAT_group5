# Exercise 8, D3: Semantic anchoring in timed automata

Draft for the report. The models are in UPPAAL 5.0 and were checked with `verifyta`: see
[uppaal/](uppaal/). The tables below describe the same automata in words. Clocks are in
seconds. All timing values that the plant does not give are estimated by the group
(restart time, message delay, how long depositing may stay stopped). `x`, `y`, `z` are local clocks. `!` sends and `?` receives on a channel.

Time constants (`internal/timing`): `HB = 5`, `KA = 5` (the broker declares a node dead after 1.5 x 5 s = 7.5 s, rounded up to `DEADAFTER = 8`), `DETECT = 15`, `RESTORE = 60`,
`ESCALATE = 45`, `P3 = 25`, `LOST = 120`, `GRACE = 3`, `RECONCILE = 5`.

## D3.1 Intra-behaviour: one TA per function block

### Node (any station)

Channels: `hb!`, `birth!`, `crash?` (from the environment), `restart?` (from Docker).

| Location | Invariant | Meaning |
|---|---|---|
| `Starting` | `x <= 1` | boots and connects |
| `Online` | `x <= HB` | publishes a heartbeat every `HB` |
| `Down` | none | crashed or frozen, silent |

| From | To | Guard | Sync | Update |
|---|---|---|---|---|
| `Starting` | `Online` | | `birth!` | `x = 0` |
| `Online` | `Online` | `x == HB` | `hb!` | `x = 0` |
| `Online` | `Down` | | `crash?` | |
| `Down` | `Starting` | | `restart?` | `x = 0` |

### Broker (death certificate)

Channels: `hb?` (any node traffic), `ndeath!`.

| Location | Invariant | Meaning |
|---|---|---|
| `Alive` | `y <= KA * 3 / 2` | the node's keep-alive is running |
| `Dead` | none | |

| From | To | Guard | Sync | Update |
|---|---|---|---|---|
| `Alive` | `Alive` | | `hb?` | `y = 0` |
| `Alive` | `Dead` | `y >= KA` | `ndeath!` | |
| `Dead` | `Alive` | | `birth?` | `y = 0` |

The invariant `y <= 7.5` forces the broker to declare the node dead within 7.5 s of its
last message. A crash is faster: the Last Will fires when the connection closes, so the
`Alive` to `Dead` edge can also be taken as soon as `crash` happens (a committed location
in the NTA).

### MON (supervisor)

Channels: `ndeath?`, `birth?`, `escalate!`, `released!`.

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
| `Escalated` | `Watching` | | `repaired?` | |

### Band section (one TA per section S1 to S5)

Channels: `halt?`, `release?`, `stop_upstream!`.

| Location | Invariant | Meaning |
|---|---|---|
| `Running` | none | |
| `Halted` | `x <= 1` | the upstream stop must be sent within 1 s (T-RC3) |
| `Stopped` | none | upstream told to stop |

| From | To | Guard | Sync | Update |
|---|---|---|---|---|
| `Running` | `Halted` | | `halt?` | `x = 0` |
| `Halted` | `Stopped` | | `stop_upstream!` | |
| `Stopped` | `Running` | | `release?` | |

### Batter (BAT-INSP and THM-DEP combined as one batch automaton)

Channels: `approved?`, `deposit_ok?`, `discard!`, `deposited!`.

| Location | Invariant | Meaning |
|---|---|---|
| `Inspecting` | `x <= 3` | inspection takes 3 s |
| `Waiting` | `x <= P3` | waits for depositing to start |
| `Deposited` | none | |
| `Discarded` | none | |

| From | To | Guard | Sync | Update |
|---|---|---|---|---|
| `Inspecting` | `Waiting` | `x == 3` | `approved?` | `x = 0` |
| `Waiting` | `Deposited` | | `deposit_ok?` | |
| `Waiting` | `Discarded` | `x >= P3` | `discard!` | |

### Station handover (sender and receiver)

Sender: `Idle`, `Stored` (document written), `Signalled`. Edge `Stored` to `Signalled`
sends `handoff!` (the message). Receiver: `Idle`, `Adopted`. Edges: `handoff?` to
`Adopted`; or, when the message is lost, after `GRACE` the reconcile pass takes the document
(`x >= GRACE` and a pass every `RECONCILE`), invariant `x <= GRACE + RECONCILE`.

## D3.2 Inter-behaviour: networks of timed automata

NTA 1, supervision: `Node(i)` x N, `Broker(i)` x N, `MON`, `Docker`, `Env`.

| Channel | From | To | Kind |
|---|---|---|---|
| `crash[i]` | Env | Node i, Broker i | broadcast (committed edge) |
| `restart[i]` | Docker | Node i | binary |
| `hb[i]` | Node i | Broker i | binary |
| `birth[i]` | Node i | Broker i, MON | broadcast |
| `ndeath[i]` | Broker i | MON | binary |
| `escalate`, `repaired` | MON, Env | | binary |

Docker is a small TA: `Idle` to `Restarting` after a delay `d` with `5 <= d <= 30` (assumed
restart time; the experiment measured at most 1.2 s), then `restart[i]!`.

NTA 2, band: `Section(1..5)`, `Station(i)`, `MON`. Channels: `halt[s]`, `release[s]`,
`stop_upstream[s]`. A halt of section s is forwarded to every section below s (one
`stop_upstream` each); MON sends `release[s]` only after `birth` of the owner (T-OR2).

NTA 3, batter: `Batch`, `THM-DEP` (accepts `deposit_ok` or not, with a stop flag), `Lost`
timer for the 120 s limit.

NTA 4, handover: `Sender`, `Receiver`, a lossy `Message` automaton that either delivers or
drops.

## D3.3 How the timing constraints enter the automata

| Constraint | Where in the TA | How |
|---|---|---|
| T-P1 heartbeat 5 s | Node, edge `Online` to `Online` | invariant `x <= HB` and guard `x == HB` |
| T-RC1 detection 15 s | Broker invariant, MON | proven by the query `A[] MON.Failed imply ...` below |
| T-RC2 recovery 60 s | Node, Docker, MON | `ESCALATE` guard on MON; query bounds restart plus birth |
| T-RC3 stop within 1 s | Band section | invariant `x <= 1` in `Halted` |
| T-RC4 batter 25 s | Batter | invariant `x <= P3` in `Waiting` |
| T-RC6 batch lost 120 s | `Lost` timer | guard `z >= LOST` |
| T-RC7 handover 3 s | Handover | guard `x >= GRACE`, invariant `x <= GRACE + RECONCILE` |
| T-EX* execution times | Station TAs | invariant `x <= d` and guard `x >= d` |
| T-OR2 release after birth | Band NTA | `release` edge guarded by `owner_online` |

Each constraint is an invariant (it forces the system to move on) or a guard (it holds
the move back). Reaction constraints that span several automata (T-RC1, T-RC2) are not
written into one automaton. They are properties of the NTA, checked with a query.

### Machine-checked models

| File | Model | Queries | Result |
|---|---|---|---|
| `uppaal/supervision.xml` | Node, Broker, MON, Docker, Env | 8 | all satisfied |
| `uppaal/band.xml` | five Sections, Env, MON | 7 | all satisfied |
| `uppaal/batter.xml` | Batch, Dep | 7 | all satisfied |
| `uppaal/handover.xml` | Sender, Message, Receiver | 4 | all satisfied |

Run (from `src/docs/uppaal`): `node gen.js` writes the models and `.q` files,
`verifyta -q supervision.xml supervision.q` checks them. `gen.js` is the source; edit it, not the XML.

What the queries prove, with the estimated parameters:

- **Supervision.** A silent node is known to MON within 8 s (`DEADAFTER`), inside the 15 s
  limit. A node is back within 60 s (a restart takes at most 30 s, estimated). MON never
  escalates (45 s). Every failure ends in recovery (leads-to). No deadlock.
- **Band.** A halt reaches every upstream section within N x 1 s = 5 s, each section hands it on
  within 1 s. A section is released only after its owner is back. After the release no section
  stays stopped.
- **Batter.** The batter never waits longer than P3 = 25 s. A batch finishes within
  6 + 3 + 25 = 34 s, far inside the 120 s limit. Deposited and discarded exclude each other. The
  discard path is reachable.
- **Handover.** A stored document is taken up within GRACE + RECONCILE = 8 s, also when the
  message is lost.

**Negative controls** (to show the queries can fail): with a restart time of up to 60 s the
60 s recovery query and the no-escalation query fail; with a batch limit of 30 s instead of
120 s the LOST query fails.

## Limits

- The models are hand-written from the code and documents and are not generated from the
  code, so they can drift. The UPPAAL check is not part of the CI.
- Each model has one node, one fault and one batch; several faults at once are not covered.
- Time is simulated seconds; the supervision values are wall-clock.
- The restart delay (1 to 30 s), message delay (1 s), and the time depositing may stay stopped
  (60 s) are estimates. The measured restart is at most 1.2 s, so the proven bounds are safe,
  not tight.
- The band model lets MON release only when every upstream section has confirmed it is
  stopped. That is an assumption about MON, not proven from the code.
