# Scalability (discussion, with tests behind the numbers)

Decision of 24 Nov: scalability is **discussed**, not built or load-tested end to end. The
numbers the design uses are backed by small tests that anyone can repeat. This page is a draft
for the report; the figures marked "measured" come from the tests named below.

## 1. What the exercise asks, and what we claim

The factory has 16 containers (14 stations, Band Transport, Monitor), one broker, one SQL
Server, one MongoDB. We claim three things:

1. The **supervision load grows linearly** with the number of nodes and is tiny at our size.
2. A plant **100 times bigger in nodes** would still be carried by one monitor process and one
   broker connection. The first limit is not the monitor.
3. What does **not** scale by adding nodes is named below (the single broker, the single
   monitor, the pan documents in one database).

We do not claim anything about more cakes per hour, more bands, or a second plant.

## 2. The load that grows with the number of nodes

Every node sends one heartbeat every T_hb = 5 s (`timing.Heartbeat`), the same for every system.
So the monitor receives `nodes / 5` heartbeats per second, nothing else grows with N in the
supervision path (the watchdog compares every node once a second).

Measured by `internal/mon/scale_test.go` (`go test ./internal/mon -run "Monitor|Watchdog" -v`),
in one process, without broker and network, on the development machine:

| Nodes | Heartbeats per second | Bytes per second | Time per heartbeat | CPU of one core |
|---|---|---|---|---|
| 16 (our factory) | 3.2 | 896 | 3.7 µs | 0.001 % |
| 160 | 32 | 8 960 | 3.5 µs | 0.011 % |
| 1 600 | 320 | 89 600 | 3.6 µs | 0.115 % |

- One heartbeat is 56 bytes on the wire (uptime, two device states, three gauges).
- The cost per heartbeat does not depend on the number of nodes (3.5 to 3.7 µs).
- A scan of 5 000 nodes by the watchdog takes about 0.6 ms, once a second.
- The test fails if one heartbeat costs more than 1 ms, if 1 600 nodes need more than 10 % of a
  core, or more than 100 KB/s.

What this does **not** include: TLS, network delay and the database writes of events. The broker itself is measured in
section 2b.

## 2b. Load test of the broker (25 Nov)

`go run ./cmd/loadtest -nodes 16,160,800,1600 -step 40s -out docs/loadtest-results.csv` starts N simulated nodes in the
Sparkplug group `LoadTest` (the factory uses `CakeFactory`, so it is not disturbed). Every node has its own connection with the
keep-alive of the stations (5 s), sends a heartbeat every 5 s (QoS 0, the 56 byte heartbeat plus a time stamp), and five
publishers add 50 reliable handoff messages per second (QoS 1). One subscriber measures the delay from publish to receive.
The real factory (16 containers) ran at the same time on the same Mosquitto, with its own traffic. One machine, one Mosquitto
with the file `deploy/mosquitto.conf`.

| Nodes | Messages per second | Lost | Connect | p50 | p95 | p99 | max |
|---|---|---|---|---|---|---|---|
| 16 | 53 | 0 | 0.0 s | 1.9 ms | 23 ms | 44 ms | 50 ms |
| 160 | 82 | 0 | 0.5 s | 1.7 ms | 35 ms | 42 ms | 49 ms |
| 800 | 210 | 0 | 3.6 s | 1.5 ms | 37 ms | 42 ms | 56 ms |
| 1 600 | 370 | 0 | 6.9 s | 6.9 ms | 27 ms | 36 ms | 65 ms |

Mosquitto used 0.1 % of a core and 10 MiB at the end. Result: 1 600 nodes (100 times the factory) and 370 messages per second
cost no message and no more than 65 ms of delay, so the broker is not the first limit at this size; the 5 s detection rule
(15 s limit) would not be touched by tens of milliseconds. What it does not show: the broker under a crash of many nodes at once
(many death certificates in one second), TLS, a network between the nodes and the broker, and a cluster. The raw data is
[loadtest-results.csv](loadtest-results.csv).

## 3. Why the other numbers are what they are

| Number | Value | Why | Backed by |
|---|---|---|---|
| Containers | 16 | one per station, so a crash or a freeze loses one station only | `containers.md`, experiment (crash and freeze, 270 runs) |
| Pans in the loop | 48 | about 17 on the line and 24 in a full cooling buffer: 41 pans can be out of the hopper | `assumptions.md` A3, `internal/fin` tests |
| Pan reserve | 100 % (48 more) | with the washer running at least 48 clean pans stay in the hopper; without it depositing goes on about 48 s | `decisions.md` 18 Nov, `internal/fin` tests |
| Pans per batch | 24 | one batch fills one pallet and the cooling buffer | `assumptions.md` section 6 |
| T_hb | 5 s | detection within 3 heartbeats = 15 s (THM-REQ-72); 3.2 messages per second is nothing for MQTT | `timing_test.go`, `scale_test.go` |
| Reconcile scan | 5 s | a missed handoff message is found within 8 s; one scan reads the pan documents of the loop (96 at most) | `station` tests, UPPAAL handover model |
| Order cycle | 22 s | estimated plant value | `assumptions.md` |

## 4. What limits growth, in the order it would hurt

1. **The broker** is one process and a single point of failure. Heartbeats are small, but
   every state signal, handoff and event passes through it. It carried 1 600 nodes without loss (section 2b), so the limit is
   availability (one process), not capacity. First step: a clustered broker or a
   bridge per production area. The Sparkplug topic space already allows a group per area.
2. **The monitor** is one process. Our test says it takes in 320 heartbeats per second at 0.1 %
   of a core, so the limit is its event handling and the SQL writes, not heartbeats. Step: one
   monitor per area and a host above them.
3. **The pan and cake documents** live in one MongoDB. The reconcile scan reads the whole
   collection every 5 s per station: 14 stations scanning 96 pans is nothing; 14 stations
   scanning 100 000 documents would need an index on the status and the station.
4. **SQL Server** holds the stock and the orders. One reservation is one atomic call. It would
   become the limit with many orders per second; the circuit breaker toward the warehouse
   (FC2) already keeps production going while it is slow.
5. **Detection time** does not grow with N: the watchdog is a linear scan once a second, the
   broker's keep-alive is per connection.

## 5. What is not tested and why

- More pans, more bands, more orders: the line is simulated with fixed station times; the
  throughput is one pan per second at depositing, so more pans only lengthen the queue.
- A load test with a failing broker or a crash of many nodes at once: listed as future work. The load of a bigger plant on a
  healthy broker is measured (section 2b).
- Everything ran on one machine at simulated station times (`team-review.md` section 3, item 9).
