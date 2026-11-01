# TODO

Everything that is not done, not verified, or not mine to decide. Items marked
**[person]** need a person or a tool that the implementation session did not have.

## Diagrams to fix (Papyrus models, **[person]**: the alignment owner)

The implementation follows the diagrams where they exist. These are the places
where the diagrams are missing, wrong, or behind the implementation.

1. **Cake Factory IBD** is empty. Add the message bus (Sparkplug B on an MQTT
   broker) and the MON part, and connect the frame ports of the slices to the bus
   (lecture draft, finding 1).
2. **BAT frame port "Fault event"** is not in the signal table. Make it internal
   (remove the frame port) or add it as a 15th signal.
3. **MON has no document and no diagrams.** Needed: blocks, IBD and state machines
   for the node watchdog (online, silent, dead), the band sections (run, slow,
   halt, fault) and the ticket lifecycle (open, repaired). The code in
   `internal/mon` can be the starting point.
4. **PLM has no state machines.** Needed: order lifecycle (released, reserved,
   held), the circuit breaker toward the warehouse (closed, open, test request),
   shipments.
5. **Rule "production may not run without the Monitor" (UC2) is not in any state
   machine.** Add a stop reason "monitor offline" to THM SM1 (admission), a hold
   to BAT Mixing and Handoff, and a gate to PLM order release.
6. **Container recovery (FC27) is modelled in no state machine.** THM, FIN and BAT
   need the behaviour "restart, reload saved state, take up cakes and pans again
   from the stored documents".
7. **BAT Mixing and Handoff** has `batchCollected`, which is not one of the 14
   signals. Either add a signal or remove the event and let the timer start when
   depositing stops (what the code does).
8. **State machines live on branches** (`state-machines-fin`,
   `requirement-diagrams-BAT`) and are not on master. Merge them.
9. **Signal descriptions**: Band state change carries five sections (S1 to S5);
   the heartbeat is also sent by PLM; events carry an event id. Update the port
   descriptions in the documents (see `docs/signals.md`).
10. **Deployment view** is missing for the report: containers, broker, SQL Server,
    MongoDB, volumes. Draw it once the container granularity is decided.

## Decisions for the group

1. **Container granularity.** Now one container per system. Two requirements
   argue for more: THM-REQ-74 (baking zone control never interrupted by other
   deployments) and BAT-NFR-06 (dosing and mixing deploy independently). The
   recommendation is those two splits only, plus one sentence in the report. The
   lecture draft's separate health-reporting container is not needed (the broker
   publishes the death certificate).
2. **Batter collected acknowledgement** (diagram item 7).
3. **PLM heartbeat**: keep it (the monitor then sees a dead PLM) or drop it
   (baseline).
4. **Parameter values** are placeholders of the simulation and need agreement:
   N_wrong = 3, P2 = 3, T_adjust = 4 s, T_hb = 5 s, P3 = 25 s, T_wait = 40 s
   (stands for 30 min), buffer capacity 8, holding area 6, S_low = 2,
   T_safe = 30 C, dose tolerance 2 %. T_resp is not used (a broken camera is
   detected at once in the simulation).
5. **Time compression.** 30 minutes of UC10 are 40 simulated seconds.

## Not verified **[person]**

1. `docker compose up` was never run (Docker is not installed on the machine used).
   Check the healthchecks (the `sqlcmd` path in the SQL Server image), the
   read-only root with the `/data` volume, and the start order.
2. The SQL Server store (`internal/store/sql_mssql.go`) and the MongoDB store
   (`internal/store/pans_mongo.go`) compile but never ran against real databases.
3. Recovery within 60 s (THM-REQ-73) and detection within 15 s (THM-REQ-72) are
   not measured in the containers. The 7.5 s death-certificate time depends on the
   broker (tested only with the embedded dev broker, keep-alive 15 s).
4. Load assumption A1 (about 50 messages per second) is not checked against
   Table II of the report.

## Code gaps

1. **CI/CD**: `.github/workflows/cicd.yml` still has the template placeholders and
   the root `Dockerfile` is the Node template. Replace with a matrix build over
   the services (`src/Dockerfile` takes `--build-arg SERVICE=`), remove
   `continue-on-error` from the test job, add hadolint and an integration test
   that starts the stack and checks that an event arrives.
2. **Tests**: only the Sparkplug codec and the signal mapping are tested. Add
   integration tests with the embedded broker (restart a service, check that no
   pan is lost or duplicated).
3. **THM resume** continues a baking pan from zone 1 (so it bakes longer), loses
   the zone temperature offset, and treats buffered cakes as hot. A cake that was
   being deposited at the moment of the crash is not resumed.
4. **MON policy**: only band faults and unresponsive nodes halt or slow the band.
   The general UC2 error flow (any error may require a halt) needs a policy table
   once the MON document exists.
5. **Duplicate filter** is in memory. After a receiver restart a duplicate is
   caught only by the checks in the receivers (batch id, pan id).
6. **PLM** keeps held orders in memory; after a PLM restart a held order is
   noticed again when BAT repeats its request, not before.
7. **Security**: anonymous broker, no TLS, SQL Server password in the compose file,
   dashboard without login. Security is not a stated quality attribute yet.
8. Event log retention, backups of SQL Server and MongoDB, clustered broker
   (accepted risk R1 in the lecture draft).
9. Real equipment (PLC, OPC UA, cameras) is out of scope: all machines are
   simulated inside the services.

## Report

1. Write the report sections that use this: the decisions (`docs/decisions.md`),
   the signal table (`docs/signals.md`), the conformance table (section 6 of
   `docs/decisions.md`).
2. Contributions table and the evaluation experiment are empty in the template.
