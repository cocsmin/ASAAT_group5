# Exercise 6.2: Containerization

Draft for the report. Everything here is in the repository and was run with
Docker Desktop.

## What is containerized and why

| Container | What | Why a container (isolation, environment, scalability) |
|---|---|---|
| `ui` | experiment dashboard, not a factory system: reads the Sparkplug messages, SQL Server and MongoDB, publishes fault injections, calls MON only for "repair confirmed" | it must keep working when MON is stopped, so it is its own container; a real plant would not need it |
| `plm-wh`, `plm-ship`, `bat-dose`, `bat-mix`, `bat-insp`, `thm-dep`, `thm-bake`, `thm-exit`, `thm-cool`, `fin-dpn`, `fin-qua`, `fin-wrap`, `fin-pal`, `fin-wash` | the 14 stations, one image (`cmd/station`, `STATION=...`) | each station fails, restarts and is deployed on its own: a crashed cooling does not stop the baking tunnel (report Tables I to VI, the "24/7 / deployability" column); Docker's restart policy is the recovery tactic of FC27 |
| `bt`, `mon` | Band Transport and the monitor, one Go binary each | the band sections are devices of BT, not stations; MON watches every container |
| `mosquitto` | the MQTT broker | the bus is infrastructure with its own lifecycle |
| `mssql`, `mongo` | the two databases (optional in the exercise, included) | stateful, with volumes, independent of the services |
| `ctl` (profile `demo`) | switches the other containers on and off for the demonstration | opt-in, because it holds the Docker socket |

**Granularity: one container per station** (decided by the group, 3 Nov). Containers
per machine would multiply processes without isolation that matters here; inside a
station the machines are Sparkplug devices with their own health state. The split
satisfies THM-REQ-74 (baking zone control never interrupted by other deployments) and
BAT-NFR-06 (dosing and mixing deploy independently) and gives every station its own
failure and recovery. The price is 16 factory containers instead of 6 and ten more
signals between the stations; see [stations.md](stations.md).

No Kubernetes: Compose is enough for one host; orchestration is only needed for
multi-host scheduling.

## Configuration files

| File | Content |
|---|---|
| `src/Dockerfile` | one multi-stage recipe for every system (`--build-arg SERVICE=thm`): Go build, then a small Alpine image, non-root user |
| `src/docker-compose.yml` | all containers, start order, healthchecks, volumes, limits, environment |
| `src/deploy/mosquitto.conf` | broker configuration |
| environment variables | `MQTT_BROKER`, `MQTT_KEEPALIVE_S`, `MONGO_URI`, `SQL_DSN`, `HEARTBEAT_SECONDS`, `SIM_SPEED`, `MSSQL_SA_PASSWORD` (see the `x-env` block of the compose file) |

Settings and the requirement or quality attribute behind each:

| Setting | Reason |
|---|---|
| `restart: unless-stopped` | recovery from a crash (THM-REQ-73, 60 s) |
| MQTT keep-alive 5 s | the broker declares a dead node in 7.5 s (THM-REQ-72, 15 s) |
| healthchecks and `depends_on: service_healthy` | start order without races; the databases and broker are ready first |
| named volume per service, mounted at `/data` | the event outbox survives a restart (THM-REQ-77) |
| non-root user, read-only root file system, `cap_drop: ALL` | smaller attack surface, fast clean restart |
| memory and CPU limits | a runaway service cannot starve the others |

## Two or more containers over a containerized bus, built and pushed by CI/CD

Nineteen containers run and talk over the `mosquitto` container (16 factory containers, the dashboard, the broker and the two databases). The pipeline
(`.github/workflows/cicd.yml`) builds the images with a matrix (one image for all stations, plus BT, MON and the dashboard) and pushes
them to `ghcr.io/<owner>/cake-factory-<image>`. The push runs on main or master
only, so it has **not happened yet**: it needs the first merge (or a manual run on
the default branch). The pipeline passes `actionlint`; the Dockerfile passes
`hadolint`.

## Impact on the quality attributes

- **Availability:** a fault stays inside one container; crash detection in under a
  second, recovery in about a second (measured, see [experiment.md](experiment.md)).
- **Deployability:** one image for all stations (the station is chosen at start), built and pushed by CI; a station is
  redeployed without stopping the others (the line pauses only through the explicit
  "no production without the monitor" rule when MON itself is down).
- **Modifiability:** the same Dockerfile and pipeline for every station; a new system
  is a new `cmd/<name>` and one compose entry.
- **Portability:** the same compose file runs on any machine with Docker (SQL Server
  image is x86; Apple silicon needs emulation).
- **Scalability:** a stateless system could run several replicas behind the broker's
  shared subscriptions; not needed or tested here.
- **Costs:** nine containers to build and watch (generated from one template); the
  broker and databases are shared dependencies; `ctl` needs the Docker socket and is
  for demonstrations only.
