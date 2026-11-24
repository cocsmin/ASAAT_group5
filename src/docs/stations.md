# One container per station

The group decided (3 Nov) to run every station in its own container instead of one
container per system. This page says what that means in the implementation. It is on
the branch `implementation`.

## The containers

| System | Container (Sparkplug edge node) | Station on the line | Machines (devices) |
|---|---|---|---|
| PLM | `plm-wh` (PLM-WH) | orders, recipe, warehouse and inventory (SQL) | OrderScheduler, RecipeManager, WarehouseInventory |
| PLM | `plm-ship` (PLM-SHIP) | transport planning and shipment | TransportPlanner |
| BAT | `bat-dose` (BAT-DOSE) | order intake, ingredient reservation, dosing | Doser, LoadCell |
| BAT | `bat-mix` (BAT-MIX) | mixing | Mixer, ConsistencySensor |
| BAT | `bat-insp` (BAT-INSP) | human inspection gate, batter handoff | InspectionStation |
| THM | `thm-dep` (THM-DEP) | depositing, band section 1 | Depositor, PanPresenceEye, CheckWeigher |
| THM | `thm-bake` (THM-BAKE) | baking tunnel with three zones, band section 2 | Zone1, Zone2, Zone3 |
| THM | `thm-exit` (THM-EXIT) | tunnel exit check, band section 3 | HeatCamera |
| THM | `thm-cool` (THM-COOL) | cooling and the fallback buffer, band section 3 | CoolingControl, CoolingBuffer |
| FIN | `fin-dpn` (FIN-DPN) | depanning, band section 4 | Depanner, VisionPicker |
| FIN | `fin-qua` (FIN-QUA) | quality inspection, band section 4 | Checkweigher, VisionCamera, MetalDetector |
| FIN | `fin-wrap` (FIN-WRAP) | wrapping and its holding area, band section 5 | Wrapper, HoldingArea |
| FIN | `fin-pal` (FIN-PAL) | cartoning, case check, pallets, band section 5 | Cartoner, CaseCheckweigher, PalletPacker |
| FIN | `fin-wash` (FIN-WASH) | pan washing, the clean-pan pool, the pan supply to depositing | WashingStation, PanPool |
| BT | `bt` | the five band sections (one container, the sections are not stations) | ConveyorController, ConveyorDrive, SpeedSensor, SectionStatusSensor |
| MON | `mon` | monitoring, event log, tickets | none |

Plus `mosquitto`, `mssql`, `mongo`, the experiment dashboard `ui` and, in the demo
profile, `ctl`. That is 16 factory containers (14 stations, BT and MON) instead of 6.
All stations are built from **one image**: `docker compose` passes `STATION=thm-bake`
and so on, and `cmd/station` starts that station. A system id (`THM`) is only a group
name; the edge nodes are `THM-DEP`, `THM-BAKE`, ... (`internal/model`).

## How a pan or cake moves from station to station

Inside a system the stations used to hand work over with function calls and channels.
Now every handoff is a Sparkplug signal, sent reliably (QoS 1, message id, duplicates
dropped) by the station that finishes and received by the next one:

| Signal | From to | Fields |
|---|---|---|
| Batch to mixing | BAT-DOSE to BAT-MIX | batchId, orderId, mixSeconds, pans, fillTarget, fillTol |
| Batter to inspection | BAT-MIX to BAT-INSP | same |
| Batter rejected | BAT-INSP to BAT-MIX | same (another mixing cycle) |
| Batch result | BAT-MIX or BAT-INSP to BAT-DOSE | batchId, result (`handed` or `discarded`) |
| Pan to baking | THM-DEP to THM-BAKE | panId, batchId |
| Pan to tunnel exit | THM-BAKE to THM-EXIT | panId, batchId, off (temperature offset) |
| Pan to cooling | THM-EXIT to THM-COOL | panId, batchId, temp |
| Cake to inspection | FIN-DPN to FIN-QUA | panId, batchId |
| Cake to wrapping | FIN-QUA to FIN-WRAP | panId, batchId |
| Cake to cartoning | FIN-WRAP to FIN-PAL | panId, batchId |

The 14 signals of the alignment baseline are unchanged; these 10 are new and exist only
because the stations are now separate. The existing signals now come from and go to
the station that does the work: for example "Batter ready" is sent by BAT-INSP and
received by THM-DEP, "Cooled cake" is sent by THM-COOL and received by FIN-DPN, "Pallet
ready for shipment" is sent by FIN-PAL and received by PLM-SHIP, and "Stop / resume
pan admission" is sent by THM-BAKE (zone fault), THM-COOL (buffer full), FIN-WRAP
(holding area full) and FIN-WASH (no clean pans) to THM-DEP, which stops depositing for
as long as any of them says stop.

**The document is the truth, the message is the trigger.** Every pan and cake has a
document in MongoDB with its station and status. Each station also scans for the
documents the station before it has handed over (`status` such as `filled`,
`tunnel-exit`, `baked`, `awaiting inspection`, `inspected`, `wrapped`, `depanned`) every
5 s and at start, and takes up what it has not got (`internal/station`). So a station
that was down, or a message that was lost, does not lose a pan. A handed-over pan is
adopted from its document only after 3 s, so that a message normally wins and a pan that
is diverted at once is never taken up.

## What changed for the band, the monitor and the dashboard

- **Band sections belong to stations.** S1 depositing, S2 baking tunnel, S3 tunnel exit
  and cooling, S4 depanning and quality check, S5 wrapping and pallets. When MON halts a
  station that is down or silent, Band Transport halts that station's section **and every
  section upstream of it** (a stopped section stops everything behind it on the band,
  the same rule as for a stopped section, FC19). Before, THM was halted as a whole.
- **Failure cases belong to stations.** FC11 is injected into THM-BAKE, FC12 and FC13
  into THM-EXIT, FC15 into THM-COOL, FC22 into FIN-DPN, FC25 into FIN-WRAP and so on
  (the catalog in `internal/model/faults.go`, the buttons on the dashboard). Every
  station has its own FC27 (container crash) and FC28 (stops responding).
- **Machine ids** start with the station: `thm-bake-zone-2`, `fin-wash-washer`,
  `bat-mix-mixer`.
- **Dashboard.** The live line colours every box by the health of its own container; the
  chips above it show every container; a system is merged from its stations for the
  lanes. Components lists every container with Stop, Start, Freeze, Restart.
- **Scenarios** run per station: `crash-thm-bake`, `crash-fin-wash`, `crash-bat-mix`,
  `freeze-thm-bake`, and so on. `band-halt` freezes the baking tunnel and expects S1
  and S2 halted and S3 to S5 running.

## What it costs and what it gives

- 16 containers and about 2 GB of memory for the factory (128 MB each) instead of 6.
- 10 more messages, all **inside** a system: they carry what used to be an internal
  transition of that system's state machine (a pan going from depositing to baking). The
  logical diagrams (system IBD, the state machines, the 14 signals between systems) still
  describe the behaviour; only the deployment view has to show the containers, and each
  system's internal ports can name the handoffs (see TODO U18).
- A station can now fail alone: a crash of the cooling does not stop the baking tunnel;
  the neighbours keep their last state, MON halts only the sections that need it. That
  is the benefit the group wanted for the containerization exercise.
- Not split further: the band sections inside BT (they are not stations), MON, and the
  machines inside a station (a machine is a device of its station, not a container).

## Open points

- **A BAT batch lives in three containers.** Mixing and inspection save every batch they receive
  and take it up again after a restart. Only a batch that dosing was still dosing when dosing
  restarted is lost (as before), and dosing discards a batch whose result never comes back
  after 120 s (`batchLostAfter`, an assumption).
- **Backpressure is gone** between the stations of FIN: quality inspection no longer
  waits when the holding area in front of wrapping is full; the holding area simply
  grows and FIN-WRAP asks depositing to stop.
- The diagrams (state machines per system, the system IBD, the signal table) have to
  be redrawn per station: TODO U18.
