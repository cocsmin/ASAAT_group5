# Report draft (Group 5), section by section

Text for `report/GroupTemplate/conference_101719.tex`. The headings are the ones of the template; the labels
(`sec:problem` and so on) are in brackets. Numbers come from the repository and are cited by file, so the group can
check them. Written 24 Nov 2026 by the implementation side; the group edits it and adds the references. The
template is about reconfigurability, our report is about availability of a cake factory, so the first sentences of
the Introduction have to be adapted by the group.

## Abstract (draft)

We designed and implemented the control software of a cake factory with six systems (production and logistics,
batter preparation, thermal processing, finishing and packaging, band transport, monitoring), 16 containers, a
Sparkplug B message bus on MQTT and two databases. Availability is the quality attribute. The design was modelled in
SysML (Papyrus), verified with five UPPAAL timed-automata models, and evaluated with 270 fault-injection runs and a
two-hour run with random failures. A stopped container is detected in 0.3 s (crash) or 7 s (freeze) and is back in
about 0.8 s; the line loses about 25 s of production per failure and no pan.

## Problem and Approach [sec:problem]

**Problem.** A production line has to keep producing when one of its controllers stops, and an overseer has to
learn about it fast. The requirement (THM-REQ-71 to -73): 99.5 % availability of the controllers, a stopped
controller detected within 3 heartbeat intervals (15 s), back in normal operation within 60 s after the cause is removed.

**Research questions.**
1. Can a Sparkplug B architecture with one container per station meet the detection and recovery times of the
   availability requirement?
2. How much production and product does a failure cost, and does the cost depend on which container fails?
3. Do the timing rules between the systems (batter wait, handoffs, warehouse requests) stay consistent when
   systems fail and restart?

**Approach.**
1. Specify the systems, signals and requirements in Papyrus (blocks, IBD, requirement and state machine diagrams)
   from the Word documents (BAT, FIN, THM, PLM) and the alignment baseline (14 signals).
2. Choose the architecture and technologies and justify them (`decisions.md`, `communication.md`, `databases.md`,
   `languages.md`, `containers.md`).
3. Implement it (Go, Docker Compose) with simulated machines, so every failure case can be shown (`demo.md`).
4. Model the timing rules in UPPAAL and verify them (`docs/uppaal`, `timed-automata.md`).
5. Evaluate availability by fault injection (`experiment.md`) and discuss scalability and security (`scalability.md`,
   `security.md`).

## Related work [sec:related_work]

Left to the group (needs the papers it has read). What our design adds to the usual picture: a message bus with
birth and death certificates (Sparkplug B) used as the failure detector, and a document per pan so a restarted
station can take up what the one before it handed over (`stations.md`).

## Use Case [sec:use_case]

A cake factory makes cakes from an order: ingredients are reserved (PLM), dosed, mixed and inspected (BAT), the batter
is deposited into pans, baked in three zones, checked at the exit, cooled (THM), then depanned, inspected, wrapped,
cartoned and palletised (FIN) while the pans are washed and used again. The band moves the pans (BT) and the
monitor (MON) watches every system, opens tickets for Maintenance and halts or restarts band sections. The use
cases of the documents include the normal production (UC1 to UC3), batter inspection (UC4), depositing and weighing
(UC5, UC6), cooling failure (UC10), damaged pan (UC12) and wrapping defects (UC14). There are 28 failure cases
(FC1 to FC28), listed with how each is triggered in [failure-cases.md](failure-cases.md): 16 are handled by the
system alone, 11 need Maintenance.

## Quality Attribute Scenario [sec:qas]

| Part | Availability scenario |
|---|---|
| Source | a container (any of the 14 stations, the band, the monitor) |
| Stimulus | the container crashes (process exits) or freezes (process stops answering) |
| Artifact | the Docker-deployed Sparkplug B architecture of the cake factory |
| Environment | normal operation, the line full, 48 pans in the loop |
| Response | the monitor detects the stop, Docker restarts the container, the container is reborn and takes up its pans and cakes from the stored documents |
| Response measure | detected within 15 s (THM-REQ-72), back within 60 s (THM-REQ-73), no pan lost, availability 99.5 % per month (THM-REQ-71) |

A second scenario covers the monitor (S4): without it no new batch starts, the line finishes what it has.
Supporting numbers and the timing relations that must hold are in [timing-constraints.md](timing-constraints.md).

## Design and Analysis Modelling [sec:design_and_analysis_modelling]

- **Structure:** six systems, fourteen signals between them ([signals.md](signals.md)), ten more between the
  stations of one system ([stations.md](stations.md)). 14 stations, the band and the monitor each run in their own
  container; SQL Server holds stock, orders, tickets and events, MongoDB holds the pan and cake documents.
- **Decisions and reasons:** Sparkplug B on MQTT (birth and death certificates are the heartbeat and the failure
  detector), SQL Server for the data that needs transactions (the atomic reservation), MongoDB for documents that
  travel, Go and Docker for small containers that restart in under a second ([decisions.md](decisions.md)).
- **Models:** the Papyrus project (`Model/PapyrusProject`): blocks, IBDs, requirement diagrams and state machines of
  BAT, THM, FIN and PLM; BT and MON as blocks and IBDs. The state machines were checked against the code on
  24 Nov ([diagram-check.md](diagram-check.md)).
- **Timing:** parameters, event chains and constraints in [timing-constraints.md](timing-constraints.md); all the
  values are assumptions of a simulation and are listed with reasons in [assumptions.md](assumptions.md).
- **Scalability and security** are discussed, not built: [scalability.md](scalability.md) (the load of heartbeats is
  linear and tiny, 0.1 % of a core at 1 600 nodes), [security.md](security.md) (out of scope, named).

## Formal verification and validation [sec:formal_v_and_v]

Five timed-automata models ([uppaal/README.md](uppaal/README.md), [timed-automata.md](timed-automata.md)):
supervision (detection and recovery times), band (a halt reaches every upstream section), batter (the 25 s and
120 s rules), handover (a stored document is taken up within 8 s, also when the message is lost) and PLM-WH (the
order and warehouse state machine). All 50 queries are satisfied (8, 7, 18, 4 and 13 per model). Findings that changed the
design: a batter could wait up to 108 s in the depositing queue and, with one rejected inspection, be deposited after
dosing had given the batch up at 120 s; since 24 Nov depositing discards a batter whose wait since approval passes
25 s, and the model proves it. UPPAAL was also used as an optimiser by bisection ([uppaal/optimization.md](uppaal/optimization.md)):
a Docker restart may take up to 43 s (30 s assumed), the reconcile period of 5 s is exactly the largest that
meets the 8 s rule, dosing's lost-batch limit could be as low as 44 s (120 s used).

## Evaluation [sec:evaluation]

### Experiment design [sec:design]

Goal in GQM form, factors and procedure in [experiment.md](experiment.md) sections 2 and 3. Factor: the container
that fails (14 stations and the band) and the way (crash, freeze; two and three stations frozen together). Fixed:
heartbeat 5 s, keep-alive 5 s, real time, one recipe, 48 pans. 15 runs per scenario, 18 scenarios, 270 runs. A second
part runs the factory for 2 h with all failure rates doubled and a simulated maintenance crew.

### Measurements [sec:measurements]

Per run: `detect_s` (fault until the death certificate), `recover_s` (until the birth certificate), `outage_s` (until
the next cake ships), `flow_resume_s`, `pans_lost` (conservation of pan documents). Resolution 100 ms, taken from the
dashboard's own API.

### Pilot test [sec:pilot_test]

A pilot of 50 runs on five systems (10 per system, 2 Nov, `experiment-results-5-systems.csv`) gave, for crash runs,
detection 0.37 s (sd 0.05 s) and recovery 0.76 s (sd 0.05 s): one or two runs would give a 95 % interval of
±0.5 s. The outage had a standard deviation of 13.5 s, because of a defect found by the pilot (after a restart the
order numbers began again and the line stood still; fixed, [experiment-aborted-run.csv](experiment-aborted-run.csv)).
With the defect fixed, 225 crash runs gave an outage sd of 1.6 s, so about 39 runs pooled give ±0.5 s; the 15 per
scenario were chosen to compare scenarios, not for one interval each.

### Analysis [sec:analysis]

269 of 270 runs passed. Crash: detection 0.33 s (max 0.43 s), recovery 0.82 s (max 1.19 s), outage 25.2 s with a 95 %
interval of ±0.2 s, no pan lost. Freeze: detection 7.0 s (limit 7.5 s from the keep-alive), inside 15 s. The outage is
a fixed refill delay of about 25 s, nearly the same for every container; with one container crash per hour it costs
about 18 minutes a month, inside the 216 minutes allowed by 99.5 %. The two-hour run at doubled rates gave 99.997 %
availability at the container level and 96 % of the maximum cake output, with 260 events absorbed. A rerun on 25 Nov (same detection and recovery, 269 of 270 runs passed) gave a mean container availability of 99.844 % and 90 % of the maximum output, with the worst container (THM-DEP) at 98.752 %, because the random failures drew 4 freezes (see [experiment.md](experiment.md) section 5.2). One run failed: the pan
count before it was taken while a pan was between two stations (47 against 48), a measurement artefact of the runner, not a lost pan ([experiment.md](experiment.md) section 5). Threats to validity: simulated
machines, one host, chosen failure rates ([experiment.md](experiment.md) section 7).

## Conclusion [sec:conclusion]

### Discussion [sec:Discussion]

The requirement is met with a margin: the detection of a crash is about 45 times and the recovery about 70 times faster than the limits (a freeze is found after 7 s, half of the 15 s). The cost of a
failure is not the detection or the restart but the refill of the line, about 25 s. A document per pan makes
restarts cheap and loses nothing; the price is that every station scans the documents every 5 s. The weak points we
found and fixed with the models and the experiment: the order numbers after a PLM restart, the batter queue of
depositing, and the 108 s wait. Known limits: one broker and one monitor are single points of failure
([scalability.md](scalability.md)), no security ([security.md](security.md)), simulated machines and times.

### Future work [sec:future_work]

A clustered broker and one monitor per area; a load test of the broker; real equipment (PLC, OPC UA); a measured
failure rate instead of chosen ones; a user interface for the overseer inside MON; the PLM, BT and MON state
machines and the Cake Factory IBD in Papyrus (see [../TODO.md](../TODO.md)).

## Contributions (from the git history, the group edits this)

| Name | Contribution |
|---|---|
| Elena Morales Gil | Go implementation, Docker setup, scenarios and experiment, UPPAAL models, documents, merge of the Papyrus models and feature model, requirement diagrams |
| Cosmin-Daniel | BAT requirement diagrams and state machines, BAT UPPAAL model fixes |
| Nick | FIN diagrams, BT and MON models and their integration in the main model |
| Niklas Braun | Papyrus project set-up, IBDs of THM and PLM |
| Alexandra Racz | PLM requirement diagrams and state machines |
