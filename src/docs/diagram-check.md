# Papyrus diagrams against the code (check of 24 Nov 2026)

What was compared: the Papyrus models on `master` after PR #9 (`Model/PapyrusProject/*.uml`: states,
transitions, triggers, guards, effects, ports) against the code in `internal/` and the signal list in
[signals.md](signals.md). The state machines were read out of the `.uml` files with a script, not by eye.
Not compared: the requirement texts (about 210 requirements), the block diagrams and the feature model.

**Important:** this is the state in git. Papyrus changes that were made on a computer but not pushed are not
in this check. If the group updated more diagrams, push them and the table below is re-checked in minutes.

## 0. Fixes of the same day (text only, checked on screen in Papyrus)

The gaps below were closed by changing the text of existing states, so no layout changed (the state boxes wrap the
longer text; the PLM texts were shortened after a first try ran over the box edges). Nothing was added or moved.

| Gap | Done | How |
|---|---|---|
| D1 | yes | FIN SM4 `Discarded` entry: a new pan replaces it after 30 s, hopper reserve 100 % of the loop |
| D2 | yes | FIN SM1 `Deciding`: 3 failed marks of the same check in a row lock the inspection until Repair confirmed (FC24) |
| D3 | yes | THM SM1 `Admission Stopped`: stop reasons now include the depositing jam (FC7) |
| D4, D5 | yes | BAT Dosing `Idle`: hold while depositing is stopped or the monitor is offline; drop a batch silent for 120 s (FC27) |
| D6 | yes | restart text (FC27) in THM SM1, SM2, SM3, SM4 and FIN SM1, SM2, SM4: reload saved state and take up pans or cakes again |
| D7 | mostly | PLM: spelling `Verifying_Inventory`; order cycle 22 s, max 2 waiting, resend after 40 s (`Idle`); order statuses (`Scheduling_Order`); 3 s, 15 s, 2 s and 15 s time values in their states; redundant "compile order and recipe targets" replaced by "record the withdrawal". **Not done:** a real transition with a timer from Idle to Scheduling_Order, and deleting the empty `StateMachine115` (both need the Papyrus GUI) |
| D10 | partly | BT ports `controllerStatus`, `transportFault`, `sectionCommand`, `targetSpeed` renamed to "Machine status (heartbeat)", "Log, warning, error and discard events", "Section command", "Target speed"; MON `eventData` renamed to "Log, warning, error and discard events". The other BT and MON ports are internal and keep their names; a "Band state change" port and "Repair confirmed" are still missing |
| D12 | yes | MON "Log Database" now has MON-FEAT-02 |
| D13 | yes | PLM `Idle`: new order only if the monitor is online |
| **D8** | **no** | PLM heartbeat and Repair confirmed ports: new ports need the IBD, to be drawn in Papyrus |
| **D9** | **no** | state machines of BT and MON: new diagrams |
| **D11** | **no** | the Cake Factory IBD (message bus and connections) |

The table in section 2 is the state before these fixes.

## 1. Matches the code

| Diagram | What matches |
|---|---|
| BAT Dosing Behaviour | reserve, dose each ingredient, top up when under tolerance (D_tol), discard when over, lock after N_dose failures, repair resumes; "the monitor is online" is in the guard of Reserving (S4) |
| BAT Mixing and Handoff Behaviour | mixing, inspection verdict, re-mix (UC4), HandingOff leaves on "depositing is working" or after T_batter into DiscardingAged (FC7), MixerFault (FC6), Restoring (FC27), "waited" time carried to THM |
| BAT Reporting | events with batch id, heartbeat every T_hb |
| THM SM1 Depositing | pan present or not (FC8), calibration guard (UC5), wrong fill counted, lock out at N_wrong (FC10), stop reasons, aged batter discarded (24 Nov) |
| THM SM2 Baking Zone | adjust for T_adjust, then zone fault and Maintenance |
| THM SM3 Tunnel Exit | 97 ± 0.5 C, camera failed diverts all, buffer full diverts all |
| THM SM4 Cooling and Buffer | cooling, increased cooling, cooling failed with buffer, repair within 30 min (40 s in the simulation) releases oldest first, later refuses |
| THM SM5 and FIN SM5 | band states run, slow, halt and restart; heartbeat and events |
| FIN SM1 Inspection Flow | safe-temperature release, depanning failure (FC21), weight, visual and metal check, hold when depanning does not answer |
| FIN SM2 Packaging, SM3 Pallet | wrapping unavailable holds cakes and stops pans when the holding area is full, refuse bin (UC14), case weight (FC26), pallet full then wrapped, "Pallet ready for shipment" |
| FIN SM4 Pan Loop | wash, dry, verify, stock, supply on request, catch-up washing after a washer fault |
| PLM state machine (PR #7) | the order states and the warehouse circuit breaker (Communication_Suspended) as in `internal/plm/machine.go` |
| Ports | the 14 signals of `signals.md` appear by name in BAT, THM, FIN and PLM where they belong |

## 2. Does not match the code (diagram behind the implementation)

| # | Where | Gap | Code reference |
|---|---|---|---|
| D1 | FIN SM4 Pan Loop | `Discarded` goes to `Final`. Since 18 Nov a damaged pan (UC12) is **replaced by a new pan after 30 s** and the pan hopper keeps a reserve of 100 % of the loop (48 pans) | `internal/fin/wash.go` (`replace`, `seedPans`), decisions 18 Nov |
| D2 | FIN SM1 Inspection Flow | repeated failure of the same check (3 in a row, FC24) locking the inspection until repair is not modelled | `internal/fin` (inspector lockout), TODO U2 |
| D3 | THM SM1 Depositing | a depositing jam (FC7, depositing stops for 30 s) is not a stop reason; the triggers of T10 are zone fault, buffer full, stop admission and band state only | `internal/thm/dep.go` (`depositOne`, reason `jam`) |
| D4 | BAT Dosing Behaviour | the 120 s rule is missing: a batch whose result does not come back is discarded and a new one is started (FC27) | `internal/bat/dose.go` (`batchLostAfter`) |
| D5 | BAT diagrams | "hold new batches while depositing is stopped" is drawn in the Mixing SM (Idle to Holding) but the code holds in dosing, before a batch is started | `internal/bat/dose.go` (loop before `makeBatch`) |
| D6 | THM and FIN state machines | container recovery (FC27: restart, reload saved state, take up pans and cakes again) is in no THM or FIN state machine (only BAT has `Restoring`) | `internal/station` (`Reconcile`), THM and FIN restart code |
| D7 | PLM state machine | still open from the PR #7 review: no timer from Idle to Scheduling_Order (order cycle 22 s, at most 2 orders not asked for), no 3 s / 15 s / 2 s / 15 s time events, order statuses, resend after 40 s, spelling `Veryfing_Inventory`, empty `StateMachine115`, "compile order and recipe targets" redundant | `internal/plm/machine.go`, TODO "PLM state machine" |
| D8 | PLM ports | no "Machine status (heartbeat)" port (PLM sends one since 3 Nov) and no "Repair confirmed" port | `internal/plm/plm.go` |
| D9 | BT and MON models | no state machines at all (band sections run, slow, halt, fault; node watchdog online, silent, dead; ticket lifecycle) | `internal/bt`, `internal/mon` |
| D10 | BT and MON ports | named differently from the 14 signals (`sectionStatus`, `logEntry`, `eventData`, ...); "Band state change", "Machine status (heartbeat)", "Log, warning, error and discard events", "Repair confirmed" do not appear under these names | `signals.md` |
| D11 | Cake Factory IBD (`ASAAT.uml`) | no ports and no connectors: the message bus (Sparkplug B on MQTT), MON, and the connections of the slices are missing | `docs/communication.md` |
| D12 | MON model | the class "MON-FD-02 Log Database" still carries `featureID` MON-FEAT-06, which was removed from the feature model on 9 Nov (Pan Spacing & Position Tracking) | `Model/FeatureModel` |
| D13 | Rule "no new batch without the Monitor" (UC2, S4) | only in the guard of BAT Reserving; not in PLM order release (PLM does not release an order while MON is offline) | `internal/plm/machine.go` |

## 3. Wording that differs but is not wrong

- BAT "Reporting": the code sends events from every station of BAT; the diagram has one block.
- Parameters are named in the diagrams (D_tol, N_dose, T_batter, T_hb, T_detect, T_adjust, T_wait, S_low, N_wrong);
  the values are in `internal/timing`, the stations and [assumptions.md](assumptions.md).
- The 10 handoffs between stations (`signals.md`, second table) exist only in the code; the diagrams keep the
  14 signals between the systems.
