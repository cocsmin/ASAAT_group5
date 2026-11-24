# Exercise 6.1: Communication technology

Draft for the report. Edit freely. Facts about the implementation are checked
against the code; judgements are marked **[judgement]**.

## Integration points

Six systems exchange the 14 signals of the alignment baseline (full table with
fields in [signals.md](signals.md)). Nothing talks to anything else directly: every
signal goes through one message broker.

| Flow | Signals | Why a bus helps |
|---|---|---|
| PLM to BAT | production order and recipe targets; ingredient request and reservation result (both ways) | the warehouse can be slow or down (FC1, FC2) without blocking dosing |
| BAT to THM | batter ready; depositing stopped / working again (back) | BAT must hold batches when depositing stops (FC7) |
| THM and FIN | pan request, pan for washing, clean pan supply, stop / resume pan admission, cooled cake | the pan loop and the cake handoff cross two systems |
| FIN to PLM | pallet ready for shipment | the business layer learns what shipped |
| all to MON | events (log, warning, error, discard), heartbeat | one place for health, traceability and Maintenance |
| BT to THM, FIN, MON | band state change | halt, slow-down and restart reach the right system within a second |
| MON to BT and the systems | section command, target speed (to BT); repair confirmed (to the owner) | the monitor decides, the band executes |

Message flow diagram (system level):

```
        PLM ----- order, reservation ------> BAT ----- batter ready ------> THM
         ^   <---- reservation result ------  ^   <-- depositing stopped --   |  \
         |                                     |                              |   cooled cake,
   pallet ready                                |                              |   pan for washing
         |                                     |                              v   v
        FIN <------ cooled cake, pan for washing, pan request ---------------+
         |  ----- clean pan supply, stop / resume pan admission ---------> THM

   every system --- events, heartbeat ---> MON --- section command, target speed ---> BT --- band state ---> THM, FIN, MON
                                           MON --- repair confirmed ---> the system that owns the machine
   all of it travels through the MQTT broker (Mosquitto); MON also publishes the host STATE
```

(The Papyrus system-level IBD with the bus is still to be drawn, see TODO.md item 1.)

## Technologies considered

| Technology | Strength | Weakness for this factory |
|---|---|---|
| **MQTT with Sparkplug B** | broker pub/sub, Last Will, retained messages, birth/death certificates, defined topic and payload semantics, tiny footprint | one broker is a single point of failure (accepted risk) |
| MQTT 5 with JSON payloads | same broker features, readable payloads | no shared semantics for presence and device state; we would define them ourselves |
| Apache Kafka | very high throughput, replay | quorum of brokers, heavy, no Last Will or retained value; throughput is not our problem **[judgement]** |
| NATS JetStream | light, persistence | presence detection must be built; less common in industry |
| RabbitMQ (AMQP) | routing, queues | exchanges and bindings add concepts; no built-in presence |
| OPC UA PubSub | industrial standard | heavy SDK and information model for simulated machines |
| REST / gRPC | simple | couples each producer to each consumer's availability: one down consumer blocks a producer, against fault containment |

## Choice and protocol

**Sparkplug B on an MQTT broker** (the group's decision). The broker is the
transport; Sparkplug defines what goes on it:

- Topics `spBv1.0/CakeFactory/<NBIRTH|NDEATH|NDATA|NCMD|DBIRTH|DDATA>/<node>[/<device>]`.
- Every system is an *edge node*, every machine a *device*. The **Last Will is the
  NDEATH certificate**: when a container dies or freezes, the broker announces it.
- MON is the *primary host application*: it publishes `STATE`; nodes republish
  their births when it comes online, and it asks a node for a rebirth when it sees
  data without a birth.
- Payload is Sparkplug B Protocol Buffers (hand-encoded in `internal/sparkplug`).
  The 14 signals keep their exact names and travel as metrics `<Signal>/<field>`.
- Delivery: handoff signals and events are QoS 1 over persistent sessions, carry a
  message or event id, and receivers drop duplicates; state signals repeat with every
  heartbeat; the heartbeat is QoS 0 (a missing one is itself the information).
- One signal for log, warning, error and discard events covers both health and
  basic logging on one technology (the group's reason for the choice).

Deviations from the Sparkplug specification and why: see
[decisions.md](decisions.md) section 1.

## Why this fits (argued from the quality attributes)

- **Availability, detection:** the broker declares a dead node after 1.5 times the
  keep-alive (7.5 s with 5 s), inside the 15 s of THM-REQ-72. Measured with real
  containers: a crash is seen in under a second, a frozen process in about 7 s
  ([experiment.md](experiment.md)).
- **Availability, recovery:** persistent sessions and QoS 1 let a restarted node
  receive what was sent while it was down; state signals are repeated, so it learns
  the current value within one heartbeat.
- **Modifiability:** producers do not know their consumers. MON was added without
  touching any producer's code path for the signals it listens to.
- **Performance:** latency is milliseconds, the requirements are in seconds
  (THM-REQ-76 and -78: one second). Throughput (about 50 messages per second,
  assumption A1) is far below what Mosquitto handles.
- **Scalability:** more nodes mean more connections and topics, not code changes;
  a clustered broker would remove the single point of failure. Not tested beyond
  fifteen nodes and 60 pans.
- **Costs:** the broker is a shared dependency (when it restarts every node
  reconnects, verified by the `broker-restart` scenario); Sparkplug adds birth and
  sequence rules; messages are binary, so tools need a Sparkplug decoder.

## Sources

Eclipse Sparkplug Specification (version 3.0); MQTT specification (OASIS, 3.1.1 and
5.0); Bass, Clements and Kazman, *Software Architecture in Practice*, 4th ed., for
the availability tactics (monitor, exception handling, rollback).
