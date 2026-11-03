# Assumptions

Everything the simulation assumes because the report, the Papyrus models or the
course material give no value. Nothing here is a fact about a real plant. Use this
list in the report ("the following assumptions were made") and to decide what to
change. Where a value lives in the code is given so it can be changed.

How to read the last column:

- **Keep**: simple, harmless, say it in the report.
- **Confirm**: the group should say yes or give a better value.
- **Real value needed**: only someone who knows the plant (or the customer) can fix it.
- **Change only if needed**: a better value costs effort and nothing in the exercises
  depends on it.

The recommendation is the simplest option that is still honest. Do not add
configuration or features for an assumption that nobody questions.

Related: [team-review.md](team-review.md) lists the decisions and bugs behind these
assumptions; time values that several systems share are in
`internal/timing/timing.go` and are checked by `internal/timing/timing_test.go`.

## 1. The product and the line

| # | Assumption | Value | Where | Recommendation |
|---|---|---|---|---|
| A1 | One product only: "Vanilla sponge" | one recipe, seeded in SQL | `internal/store/sql_mem.go`, `sql_mssql.go` | Keep. The report describes one line, not a catalogue |
| A2 | Pans per batch | 30 (was 6: the line stood empty most of the time) | recipe seed `PansPerBatch` | Keep. Depositing at 1 pan per second is busy for 30 s per batch, so the line carries about 17 pans at once |
| A3 | Pans in the pan loop | 60 | `PAN_COUNT` in `docker-compose.yml` | Keep. About 17 are in the line, the rest wait clean in the pool |
| A4 | Fill target and tolerance of the batter in a pan | 450 g +/- 5 g (1.1 %) | recipe seed | Keep. Chosen, see section 5: well inside the legal tolerance of the packaged product |
| A5 | Dose tolerance of every ingredient | 1 % (was 2 %); the load cell scatters 0.25 % | recipe seed, `internal/bat/dose.go` | Keep. Chosen, see section 5: industrial batching is accurate to about 0.3 to 1 % |
| A6 | Ingredients and stock | recipe grams are 5 x the old ones (flour 5000 g for 30 pans); 45000 g of each ingredient at the start; restock after 15 s with 2 x the recipe + 25000 g | seed, `internal/plm/plm.go` | Keep. Same ratio as before: stock for about nine batches |
| A7 | Mixing time | 6 s | recipe seed | Change only if needed |
| A8 | A new order every 22 s, at most 2 not yet picked up | derived from the stage times | `internal/plm/plm.go` | Keep. It sets the pace of the whole line |
| A9 | Pallet | 30 cakes | `PALLET_SIZE` | Keep (equals the pans per batch, so one batch fills one pallet) |
| A10 | Trucks | three, assigned round-robin | `internal/plm/plm.go` | Keep. Nothing in the exercises depends on it |
| A11 | Cake core temperature at the tunnel exit | 97 C +/- 0.5 C | `internal/thm/thm.go` (UC9 business rule) | Keep, it comes from the use case |
| A12 | Temperature at which cakes may be packaged | 30 C core temperature ("room temperature") | `tSafe` in `internal/thm/thm.go` | Confirm. No source found for a number, see section 5 |
| A13 | Cooling buffer capacity | 30 pans (one batch) | `internal/thm/thm.go` | Change only if needed |
| A14 | Holding area before packaging | 6 cakes | `internal/fin/fin.go` | Change only if needed |
| A15 | Human inspection of the batter | simulated, 3 s, always "good" unless UC4 is injected | `internal/bat/bat.go` | Confirm. A button on the dashboard would make it real; only worth it if the demo needs it |

## 2. Time

All production durations are *simulated* seconds (with `SIM_SPEED=1` they equal real
seconds). The supervision timers (heartbeat, keep-alive, timeouts) are real
(wall-clock) time. The split is deliberate.

| # | Assumption | Value | Recommendation |
|---|---|---|---|
| T1 | The simulation runs in seconds, not in plant time; Table II of the report gives no belt speed, baking time or throughput | seconds | Keep, and say so in the report. Real baking and belt figures depend on the oven, the pan and the batter, so none is invented (section 5) |
| T2 | UC10's 30 minutes (cooling failure to repair; buffer wait limit) are compressed | 40 s, a fixed factor of 45 | Keep |
| T3 | Step times | depositing 1 s per pan; three baking zones 2 s each; exit check 0.5 s; cooling 4 s; depanning, inspection, wrapping, cartoning 1 s each; washing 0.6 s + drying 0.4 s (a continuous washer, 1 pan per second like every other FIN step); dosing 1.2 s per ingredient | Change only if needed. They are chosen so that the picture is watchable |
| T4 | Heartbeat T_hb | 5 s for every system, PLM included | Keep. Decided by the group |
| T5 | Detection of a stopped controller | 15 s (3 x T_hb, THM-REQ-72); the MQTT keep-alive (5 s, dead after 7.5 s) is an extra and faster | Keep |
| T6 | A container that does not restart by itself | escalated to Maintenance after 45 s (THM-REQ-73 allows 60 s) | Keep |
| T7 | Batter that waits too long for depositing is discarded | 25 s (BAT parameter P3) | Confirm |
| T8 | Retries | ingredient request repeated after 20 s; order sent again after 40 s; every database call times out after 3 s; event outbox retried every second; state saved every 3 s | Keep. The test in `internal/timing` checks that they fit together |
| T9 | Faults the system repairs by itself, injected for the demo | depositing jam 30 s; no-pan wait 4 s; recalibration 6 s; wrong speed corrected after 10 s; band sections restart 1.5 s apart | Keep |

## 3. Architecture and operation

| # | Assumption | Value | Recommendation |
|---|---|---|---|
| S1 | One container per station (14 stations, plus BT and MON), plus broker, SQL Server, MongoDB; the dashboard is a separate demo container | decided by the group (3 Nov), see stations.md | Keep |
| S2 | The dashboard (`ui`) is for the experiments and the demo, not part of the real plant | MON has no user interface | Confirm (see team-review section 2, item 16) |
| S3 | MongoDB holds the traveling pan documents and the saved state of the nodes; the group treats it as experiment data | `mongo-down` scenario dropped | Keep |
| S4 | "No production without the monitor": nothing new starts while MON is offline | THM admits no pans, BAT starts no batch, PLM releases no order | Confirm. It means a MON restart pauses the line |
| S5 | The broker is anonymous, no TLS; SQL Server uses a development password | security is not a stated quality attribute | Keep, and name it in the report as out of scope |
| S6 | The dashboard buttons play Maintenance: anyone can confirm a repair; no login, no role | | Keep for the demo |
| S7 | The `ctl` container (demo profile) can control every container on the machine through the Docker socket | opt-in | Keep, opt-in only |
| S8 | Sparkplug B is used with deviations: nodes read each other's data messages directly; QoS 1 and persistent sessions for handoffs; state is repeated with the heartbeat instead of MQTT retained messages | see decisions.md section 1 | Confirm |
| S9 | Every handoff carries a message id and every event an event id, so receivers drop duplicates | not in any port description | Keep |
| S10 | Failure injection (`Fault injection`) and `Node Control` messages exist only for the demo | outside the 14 signals | Keep |

## 4. The experiment

| # | Assumption | Value | Recommendation |
|---|---|---|---|
| E1 | Availability counts the time the controller does not answer, not the time the line takes to produce again | about 1 s per crash; production recovery reported next to it | Keep |
| E2 | Assumed fault rate | one fault per hour, giving about 99.97 % for a 1 s outage | Confirm and name it in the report |
| E3 | Which faults are measured | container crash and frozen process; the Band Transport crash is in the scenario list but its rerun is still to do | Keep |
| E4 | Runs | 10 per scenario, one machine, real speed (`SIM_SPEED=1`) | Keep |
| E5 | THM-REQ-76 and -78 (stop new pans and react to the band within 1 s) are not measured | | Keep unmeasured unless the course asks for it |

## 5. Where the realistic values come from

The values that needed a real number were chosen by the implementation after a look at
public sources. The sources are partial: they give orders of magnitude, not a standard
for this plant, so the figures stay assumptions.

- **Fill weight, 450 g +/- 5 g (A4).** The legal rule for prepackaged goods (EU directive
  76/211/EEC) lets a single package of a nominal 300 to 500 g be short by at most 3 %,
  which is 13.5 g for 450 g (table as reproduced by the [Austrian metrology
  authority](https://bev.gv.at/en/Topics/Metrology-Service/Legal-Metrology/Prepackages.html);
  the directive text is on [EUR-Lex](https://eur-lex.europa.eu/eli/dir/1976/211/oj) and
  has been amended, check the current Annex I before quoting). The rule applies to the
  packaged product, which weighs less than the batter after baking, so 5 g on the batter
  is a conservative internal limit, not the legal one.
- **Dose tolerance, 1 % (A5).** Vendor and trade material gives about 0.3 % for blending
  bakery ingredients and +/- 0.25 % to +/- 1 % for dosing belt scales, and about 1 % for a
  small liquid dose ([Baidu b2bwiki](https://b2bwiki.baidu.com/en/encyclopedic/weighing-and-batching-system),
  [Baking Business](https://bakingbusiness.com/articles/52058-scaling-transfers-critical-to-minor-micro-ingredient-accuracy),
  [Instrumentation.co.uk](https://www.instrumentation.co.uk/raising-accuracy-in-dosing-improves-product-quality-for-bakery/)).
  The old 2 % was looser than any of them; 1 % is the upper end of the range, and the
  simulated load cell scatters 0.25 %, so a normal weighing almost never fails and a
  failure case is always an injected one.
- **Cooling before packaging, 30 C (A12).** Sources agree that cakes must be cooled to a
  stable core temperature before slicing and packaging and that packing too warm makes the
  packaging warp and leak ([Buhler](https://www.buhlergroup.com/global/en/campaigns/NAM_Solutions_for_cake.html),
  [Making.com](https://making.com/equipment/sponge-cake-cooling-tunnel)), but I found no
  number. 30 C is a plausible round value near room temperature; it is the one value in
  this list that nobody has backed with a source, and the group should confirm it.
- **Baking time, belt speed, throughput (T1).** The same sources say that temperature and
  belt speed are set together for each product, pan size and oven, so there is no single
  real figure. Typical sponge cakes bake for tens of minutes; that is general knowledge,
  not something the search confirmed. The simulation therefore stays in seconds (T1, T2)
  and its throughput is not a plant figure.
- **Core temperature at the tunnel exit, 97 C +/- 0.5 C (A11).** Comes from the use case,
  not from a source.
