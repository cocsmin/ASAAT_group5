# Exercise 6.3: Programming languages

Draft for the report.

## Decision

**Go for every system and subsystem** (the group's decision), plus the minimum of
other languages that the platform forces. Not literally "Go for everything":

| Part | Language | Note |
|---|---|---|
| PLM, BAT, THM, FIN, MON and all their subsystems | Go | one toolchain, one Dockerfile, one CI build |
| Dashboard in the browser (`internal/mon/web`) | HTML, CSS, JavaScript (no framework, no build step) | the browser requires it; served by the Go monitor |
| Database schema | T-SQL (SQL Server) | embedded in Go |
| Deployment | YAML (Compose, GitHub Actions), Dockerfile | configuration, not code |

## Per system and subsystem

All subsystems below are implemented in Go; the argument is the same for each, with
the points that matter for it.

| System | Subsystem | Why Go fits | Trade-off |
|---|---|---|---|
| Batter Preparation | Ingredient Intake & Dosing, Mixing Control | not 24/7-critical; goroutines model dosing, mixing and the human gate as simple sequential code | none that matter at second-range timing |
| Thermal Processing | Depositing, Baking Zone, Cooling, Tunnel-Exit Inspection | 24/7 critical: static binary, starts in well under a second (recovery 60 s), many concurrent pans as goroutines | garbage collection gives no hard real time; the fast temperature loop belongs to the device (heaters keep their last command if the container dies, THM deployment table) |
| Finishing & Packaging | Depanning, Quality Inspection, Wrapping & Cartoning, Pan Washing & Supply | concurrent stages connected by channels map directly to the conveyor | vision and metal-detector SDKs may be C++; wrap them behind a small service when they exist |
| Band transport and Line Monitoring & Health | Section coordination, drive supervision, health detection, event logging, maintenance | the monitor must stay up when others fail: small, crash-resistant, one binary; built-in HTTP server for the dashboard | none |
| Production & Logistics | Order & Scheduling, Recipe, Warehouse & Inventory, Transport & Shipment | good SQL and driver support (`database/sql`, go-mssqldb) | an ORM-style productivity is lower than in Java or C#; not needed for four tables |

## Options considered

| Language | For | Against here |
|---|---|---|
| Go | simple, static binary, small image, goroutines, compile-time types, good MQTT/SQL/MongoDB drivers | GC pauses; fewer industrial SDKs |
| C or C++ | hard real time, vendor SDKs | memory safety, slow to write and review, not needed at second-range deadlines |
| Rust | safety and determinism | harder to read and review; slower for a student team |
| Java or C# | strong ecosystems, mature OPC UA libraries | heavier runtime and image, slower start (recovery bound) |
| Python | fastest to write, good for data and vision | errors at run time, slower start, weak concurrency for many devices |
| JavaScript or TypeScript | only option in the browser | used for the dashboard only |

## Impact on the quality attributes

- **Availability:** static binary and fast start give about one second recovery
  (measured, [experiment.md](experiment.md)); type checks catch a class of errors
  before they become crashes. A garbage-collected language cannot guarantee hard
  deadlines; the report's deadlines are 1 s and above.
- **Deployability and modifiability:** one language, one Dockerfile pattern, one CI
  job, shared libraries (`internal/node`, `internal/sig`, `internal/store`).
- **Performance:** ample for about 50 messages per second; the dashboard updates over
  server-sent events without a framework.
- **Maintainability for a student team:** a small language that reads easily in
  review.

## Code snippet: one stage, same pattern in every system

The baking tunnel moves a pan through three zones; a band halt pauses it, a slow-down
doubles the time (`internal/thm/thm.go`):

```go
func (t *THM) span(section int, d time.Duration) bool {
	if !t.gates[section].Wait(t.Ctx) { // blocks while the band section is halted
		return false
	}
	if t.slow[section].Load() {
		d *= 2
	}
	return sim.Sleep(t.Ctx, d)
}
```
