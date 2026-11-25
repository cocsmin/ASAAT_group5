# Presentation script (guion) and possible questions

The deck is [cake-factory-5min.pptx](cake-factory-5min.pptx). Nine slides for 5 minutes, then two
appendix slides that are only shown if the professor asks. Speaker notes with the key point of each slide are also
inside the pptx. Numbers are from [../experiment.md](../experiment.md); if the rerun changes them, change the
`DATA` block at the top of `build-deck.js` and run it again (`NODE_PATH=<folder with pptxgenjs> node build-deck.js`).


| Slide | Time | Message |
|---|---|---|
| 1 Title | 0:10 | The question of the talk |
| 2 Goal | 0:20 | Availability, three numbers from the requirements |
| 3 Implementation | 0:35 | How it is built and what happens when a container dies |
| 4 Control UI (GIF) | 0:30 | The real dashboard during a crash |
| 5 UPPAAL (GIF) | 0:40 | The model played step by step, the defect it found |
| 6 Papyrus | 0:15 | Structure and behaviour models match the code |
| 7 Exercise 8 | 0:40 | Experiment design |
| 8 Exercise 9 | 0:55 | Results |
| 9 Scalability and conclusion | 0:35 | Load test, conclusion, limits |

Total about 4:40, which leaves 20 seconds of margin. The weight is on what the professor asked to see:
the experiment (slides 7 and 8), the implementation (slides 3 and 4) and UPPAAL with its simulation (slide 5).
The GIFs play only in slideshow mode (F5), not in the editing view. If you run over, cut slide 6 to one sentence.

## Script

**Slide 1 (0:10).** Good morning. We are group 5. Our cake factory is six systems running in sixteen Docker
containers. Our question is simple: what happens when one container dies? How fast do we notice, how fast is it
back, and do we lose cakes?

**Slide 2 (0:20).** We chose availability as our main quality attribute, because the THM requirements give numbers
for it: 99.5 % availability per month, a stopped system detected within 15 seconds, and back in normal operation
within 60 seconds. We checked this in three steps: we built it, we modelled it with Papyrus and UPPAAL, and we broke
it on purpose in an experiment.

**Slide 3 (0:35).** The implementation is a set of Go programs, one container per station, sixteen in total, plus
the MQTT broker, SQL Server and MongoDB. All 14 signals between the systems travel through the broker as
Sparkplug B messages with QoS 1 and persistent sessions. SQL Server holds orders, recipes and stock; MongoDB holds
one document per pan, so a pan can be found again after a crash. The bottom row is the story of every experiment run:
the process ends, the broker publishes the death certificate, MON sees it, Docker restarts the container, the station
reads its state back from the database and sends a birth certificate. Birth and death certificates are our failure
detector, so we need no extra heartbeat protocol.

**Slide 4 (0:30).** This is the real control UI of the factory, a dashboard on port 8085. The coloured chips show every
system online or down and how often it restarted. The animation is a real moment of the experiment:
BAT-INSP was crashed, MON saw its death certificate, Docker restarted it, and the event log shows it back with its
last saved state loaded, within a second. In the event log you also see the depositing station discarding a batter that waited 25 seconds: that is the rule UPPAAL checked on the next slide. Below the line you see the pans moving through the factory, and on the
right the event log stored in SQL and the maintenance tickets. The components panel can stop, freeze or restart
any part: that is exactly how the experiment injects its faults. If there is time, show the live UI.

**Slide 5 (0:40).** Now UPPAAL. This animation is a real trace from UPPAAL, played step by step: we asked the
model whether a queued batter can expire. The batch is mixed and inspected, depositing stops, the batter is handed
over and waits in the queue. Depositing starts again, but the batter has already waited 26 seconds, more than
T_batter of 25, so the state Expired is reached. In the first design this wait could even be 108 seconds. The fix is in
the code: THM-DEP discards a batter that waited too long. In total we have five models and 50 queries, all
satisfied, and by bisection we know the margins, for example a Docker restart may take up to 43 seconds while the
code assumes 30.

**Slide 6 (0:15).** The Papyrus models are the structure, the internal block diagram of the six systems with
their ports, and the behaviour, one state machine per system. When model and code disagreed we changed the code.

**Slide 7 (0:40).** Exercise 8, the experiment. We use the GQM template: analyse the Docker-deployed Sparkplug B
factory, for evaluating availability, from the point of view of the production overseer. Four metrics: time to
detect, time to recover, production outage and pans lost. 18 scenarios, 15 runs each, 270 runs. Fifteen scenarios
crash one container, one freezes the baking tunnel, and two freeze two or three stations together. Each run waits
until the factory is settled, injects the fault through the dashboard API, polls every 100 milliseconds and measures.
Afterwards we ran the factory for two hours with doubled failure rates.

**Slide 8 (0:55).** Exercise 9, the results. 269 of 270 runs passed; the one failure was a frozen BAT-MIX that came back after 70 seconds instead
of 60, a timing race we did not investigate. A crash is detected in 0.3 seconds, a freeze in 6 to
8 seconds, both inside the 15 seconds. Recovery took at most 1.2 seconds, the limit is 60. No pan was lost.
In the two-hour run with doubled random failures the mean availability of the containers was 99.844 %, above 99.5 %, but the worst container, THM-DEP, was at 98.75 %, below it. The line made 3252 cakes per hour, 90 % of the maximum. The
interesting result is the outage: about 25 seconds for almost every container. The line has to refill, so refill
time, not detection or recovery, decides the availability.

**Slide 9 (0:35).** We also tested the broker with up to 1600 simulated nodes, each sending a heartbeat every
5 seconds, plus 50 handoffs per second. No message was lost and the 99th percentile delay stayed under 45
milliseconds. So the first limit of the design is availability, not broker capacity. To conclude: detection and recovery are met, and the 99.5 % is met on average but not for the worst container, and model, code and experiment agree: UPPAAL found one defect and the
experiment found another one, a queue restore that blocked THM-DEP, which we fixed. The limits are that the machines
are simulated, everything runs on one host, the failure rates are our choice, and 15 runs per scenario. Next
steps are a real network, a broker cluster and measured failure rates. Thank you, we are happy to answer questions.

## Possible questions and answers

### About the experiment

**Why 15 runs per scenario?** A pilot of 50 runs on five systems showed small spread for detection and recovery
(standard deviation 0.05 s). The outage had a large spread (13.5 s) because of a defect that we fixed; after the
fix it was 1.6 s. With that spread about 39 runs would give a 95 % interval of plus or minus 0.5 s for the outage.
We used 15 per scenario, but the 14 stations show the same outage, so together the data is much larger than 15.

**Why crash and freeze as faults?** They are two different mechanisms. A crash closes the TCP connection, so the
broker publishes the death certificate at once (Last Will). A frozen process keeps the connection open and is only
found when the keep-alives stop, after 1.5 times the keep-alive of 5 s, 7.5 s. Multi-freezes test whether the band
halts correctly when several stations fail together.

**Why is the outage 25 s if detect and recover are about 1 s?** After the restart the line has to refill: batter and
pans in flight travel on and the next order comes on its own 22 s cycle. The spread is small, so it is a fixed delay
of the simulated line and does not depend on which container fails.

**Is the availability requirement met?** Only partly shown. In the 2 h run at doubled rates the mean container availability was
99.844 %, above 99.5 %, but the worst container (THM-DEP) was at 98.752 %, below it, and two hours are not a month. The first run
on 3 Nov gave 99.997 %, but that was a lucky draw: the failures are random, and the rerun drew 4 freezes and 16 crashes instead
of 0 and 10. A freezing container stays down until the broker declares it dead and the crew repairs it, up to 55 s. What the run
shows is that the line keeps producing (90 % of the maximum, no pan lost) while containers fail. The per-fault check is clear:
detect at most 8 s, recover at most 1.2 s.

**What about the one failed run?** In the rerun it is `freeze-three-stations`, run 1: BAT-MIX was frozen together with two
other stations and came back after about 70 s, the limit is 60 s. The broker sent a second death certificate and then the
birth certificate arrived. The other 14 runs passed and on 3 Nov all 15 passed. We did not find the cause; our guess is a
race between the old frozen connection and the new one of the same client, or load on the test machine. On 3 Nov the one
failed run was a pan count taken while a pan was between two stations (a measurement artefact).

**Why are some outages much longer than 25 s?** 6 of 269 runs had an outage above 35 s (up to 85 s), all in `crash-plm-wh`
and `crash-bat-dose`, all in the first 25 minutes, while we were also using the machine for other work. We did not
investigate it, so we cannot say if it is a real tail or load. The median stays 24.9 s and detection and recovery are not
affected.

**What are the threats to validity?** Simulated machines with compressed process times (the 30 minutes of UC10 are
40 s); one machine and one Docker host, so no network delay; failure rates are assumed, not measured; the tail
beyond the 95th percentile is not seen with 15 runs; "recovered" means birth certificate received, not full
capacity; the runner polls every 100 ms, so times have that resolution.

**How is `pans_lost` measured?** The number of pans in the loop (documents in MongoDB) before the fault and after
the system is back. A pan that is between two stations at the moment of counting is not visible; this is the
limit of the metric.

**Why not use the real failure data?** We have no real line. The rates come from a catalog that the group fixed and
doubled in the long run, so that two hours stand for four.

### About the models

**What did UPPAAL give you that the experiment could not?** The experiment only tests the cases we inject. UPPAAL
checks all behaviours of the model, for example that a batter never waits longer than allowed, including interleavings
that are hard to hit by chance. It found the 108 s wait that an experiment would see only by luck.

**Is the UPPAAL model the same as the code?** No, it is an abstraction of the timing and the messages. We kept it
close: the constants have the same names as in the code (T_batter, LOST, DEADAFTER) and the finding was moved into
the code and its tests. Estimated values are marked in the model files. What it does not cover is in
`docs/verification-scope.md`.

**What does the negative margin on BLOCKMAX mean?** At the estimated stop of 60 s, the late discard at depositing
comes after dosing already gave the batch up, so it only drops a batch dosing replaced; nothing is deposited from
it. It is not a failure of the check that nothing bad is deposited (the `Took` query holds for any stop length).

**Why is the Papyrus deployment diagram missing?** We made it as an SVG (`docs/deployment.svg`) because it is
described by the Docker Compose file; drawing it again in Papyrus would not add information. The message bus is also
not drawn in the Papyrus IBD, because every signal goes through the broker and the connectors would become
unreadable. Both are written down in `docs/diagram-check.md`.

### About the architecture

**Why Sparkplug B and MQTT?** The birth and death certificates give failure detection and state without extra
code, QoS 1 with persistent sessions keeps messages for a station that restarts, and the topic namespace is
defined by the standard. It is also common in industrial automation.

**Why one container per station?** A failure then affects one station, Docker restarts exactly that one, and the
experiment can inject faults per station. The cost is more containers and more connections to the broker.

**Why both SQL and MongoDB?** SQL Server for data with a fixed structure and relations (orders, recipes, stock,
shipments, event log). MongoDB for the pans: each pan is one document that changes during its life and is read as
a whole. The ER diagram in the appendix shows the SQL side.

**What happens to a message when a station is down?** With QoS 1 and a persistent session the broker keeps it
until the station reconnects. Some stations also store their own queue in the database and read it back at start
(BAT-MIX, BAT-INSP, THM-DEP); before this, a BAT-INSP crash cost 135 s, now about 25 s.

**What is the single point of failure?** The broker, SQL Server and MongoDB. In the experiment we only fail the
factory containers. `docs/scalability.md` and `docs/security.md` discuss a broker cluster as the next step.

**How does it scale?** The load test used 16 to 1600 simulated nodes with a 5 s heartbeat and 50 handoffs per
second. All messages arrived and the 99th percentile delay was under 45 ms, so the broker is not the first limit.
The connection time grows with the number of nodes (about 7 s for 1600).

### About the presentation itself

**Why did you choose availability as the quality attribute?** The THM requirements give numbers for it (99.5 %,
15 s, 60 s), so it can be measured and the experiment has a clear pass or fail. It is also the attribute that the
architecture decisions (one container per station, Sparkplug B certificates, saved state) mainly serve.

**What was the main result in one sentence?** A dead container is noticed in under a second (a frozen one in 7 s),
it is back in about a second, no pan is lost, and the line needs about 25 s to refill.

**What is the GIF with the UI showing?** A recording of our own dashboard while the experiment crashes BAT-INSP:
the warning, MON seeing the death certificate, Docker restarting the container, and THM-DEP discarding a batter that
waited more than 25 s. It is a real run, not a mock-up.

**Is the UPPAAL animation the UPPAAL simulator?** It is the real witness trace that `verifyta` prints for the query
`E<> Dep.Expired`, drawn step by step by a script. We did not use the UPPAAL GUI simulator, but the states,
transitions and clock values are exactly those of the trace.

**Why did you not show all diagrams?** Five minutes. The appendix has the ER diagram and the UPPAAL templates and
margins; the other diagrams (deployment, the other state machines, the other UPPAAL models) are in the report and in
the `docs` folder.

**How did you make sure the diagrams are correct?** The diagrams were drawn by the group, and each one was then checked
against the code and against the other diagrams (`docs/diagram-check.md`). When a diagram and the code disagreed, we
fixed one of them and wrote the decision in `docs/decisions.md`.

### About the project in general

**What is the system?** A simulated cake factory in Go: six systems (PLM, BAT, THM, FIN, BT, MON) on 16 containers,
MQTT broker with Sparkplug B, SQL Server for orders, recipes, stock and the event log, MongoDB for the pans, a
dashboard for the overseer.

**What are the main architecture decisions?** One container per station; all signals through the broker (no direct
calls); birth and death certificates as failure detector; state saved in a database so a restarted station loads it;
doubled safety nets for food safety (a batter is discarded after 25 s). They are listed in `docs/decisions.md`.

**Which requirements did you not meet or only partly meet?** Say it openly: the machines are simulated, availability
the availability figure holds for assumed failure rates, there is no real network and no broker cluster, security is described in
`docs/security.md` but not fully implemented. Check `docs/verification-scope.md` and `docs/team-review.md` for the list.

**How do you know the code does what the models say?** Papyrus and UPPAAL were checked against the code
(`docs/diagram-check.md`), and when they disagreed the code or the model was changed and written in
`docs/decisions.md`. The UPPAAL finding about the batter is covered by unit tests in `thm/stations_test.go`.

**How would it behave with a real factory?** Process times are compressed, so absolute outage times (25 s) are only
valid for the simulation; detection and recovery depend on the software and the broker and would be similar. A real
line needs measured failure rates and a test on separate machines with a real network.

**What was the hardest part?** Making a restart really safe: a station that comes back must find its queue and its pans
again and must not resurrect old batter. The rerun showed it: THM-DEP got stuck restoring a queue of 1025 stale
batches. Keeping the models and the code consistent was the second hardest part.

**What would you do with more time?** Broker cluster, measured failure rates, more repetitions for the scenarios
with a long tail, running the experiment on several machines.

### About the process

**What did you find with the rerun?** Two things. Before the experiment: THM-DEP got stuck at start because it tried to
restore a queue of 1025 stale batches into a channel that is smaller; we made the restore not block and a batter of unknown
age count as expired. In the experiment: detection and recovery are the same as on 3 Nov (crash 0.34 s, at most 1.23 s to
recover), 269 of 270 passed, and we see a few long outages and one slow recovery that we did not explain.

**What would you do differently?** Measure real failure rates, run on separate machines, and run more repetitions
for the scenarios with a long tail (`crash-fin-pal` and others showed single runs about 5 s longer; the cause is not
investigated).
