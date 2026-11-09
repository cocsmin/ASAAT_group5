# Exercise 8, D2: Scope of verification and validation

Draft for the report. The group decides; each choice has a reason.

## 1. What is formally specified and model-checked

| Part | Why | Properties to check |
|---|---|---|
| **Supervision protocol** (node, broker, MON): heartbeat, death certificate, restart, escalation | Availability is the main quality attribute and it has numbers (THM-REQ-72, -73). It is small and mostly timing, which timed automata are good at. | A stopped node is detected within 15 s. A restarted node is back within 60 s. MON never escalates before 45 s. No deadlock. |
| **Band Transport** with its five sections and halt release | A safety-related rule with ordering: a halted section halts everything upstream, and a section is released only after its owner is back. Several stations interact. | Never two pans on one place. A halt reaches every upstream station within 1 s. A halt is always released after the owner's birth. |
| **Batter handoff** (BAT-INSP to THM-DEP) with the 25 s and 120 s limits | Food safety: batter must not wait for ever. Two timers interact. | Before the handoff a batter holds at most P3 (+1 s polling), then it is discarded. A batch is never both discarded and deposited. A batter is deposited before dosing gives the batch up (120 s). The model follows the code: no acknowledgement from depositing. It also shows that after the handoff nothing limits the wait (`team-review.md`, section 3). |
| **Station handover** (document stored, then signal, then adoption after 3 s grace) | The core of the "no pan is lost" claim. | A pan document is always owned by exactly one station, or in transit for at most 8 s. |
| **PLM-WH order and warehouse flow** (order cycle, ingredient request, withdrawal, held order, warehouse timeout with circuit breaker) | The state machine of the Papyrus model is in the code, and BAT waits for its answers. Several timers interact: 22 s order cycle, 3 s withdrawal, 15 s restock, 2 s timeout, 15 s breaker, 20 s repeat of BAT. | At most two orders wait for BAT; at most one order per cycle; BAT is confirmed only after the withdrawal; BAT gets its final answer within 18 s; a held order and a suspension always end. |

## 2. What is not formally specified

| Part | Reason | What covers it instead |
|---|---|---|
| Dosing, mixing, baking, cooling and finishing step logic | Linear sequences with fixed durations; a timed automaton would only repeat the code. | Unit tests per package and the 28 scenarios. |
| Cooling failure and the 30 minute decision (UC10) | Same pattern as the batter timer; one model is enough as the example. | Scenario `cooling-chain`. |
| MSSQL and MongoDB internals | Standard products. | Only the call timeout (3 s) is modelled, as a delay. |
| Random failures and rates | The rates are assumptions, not behaviours to verify. | The availability run. |
| The dashboard | Demo only. | Manual check. |
| Security | Not a stated quality attribute (`assumptions.md`, S5). | None. |

## 3. How

1. **Specify** each chosen part as timed automata in UPPAAL, one TA per function block
   and one NTA per part (`timed-automata.md`).
2. **Verify** the properties with UPPAAL queries (`A[]`, `E<>`, `A<>`, leads-to). The
   queries are in `timed-automata.md`.
3. **Validate** the model against the implementation: the measured values of the
   experiment (`experiment.md`) must lie inside the bounds the model proves, and the
   scenarios must show the same behaviour as the model's traces. If the measured
   detection time exceeds the proven bound, the model is wrong.
4. **Document** every simplification (for example: time is discrete, one pan at a time,
   no message loss unless modelled).

## 4. Limits

- The models are not generated from the code. They are hand-made and can drift.
- The UPPAAL models in `docs/uppaal/` were checked with `verifyta`; the check is not part of the CI.
- One machine, simulated process times: proven bounds hold for the model's parameters only.
