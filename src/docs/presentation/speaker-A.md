# Speaker A: slides 1 to 4 (1:35)

You present: 1 Title, 2 Goal, 3 Implementation, 4 Control UI. This page explains the project from zero, gives you what to say
(you can read it, but try to say it in your own words), and the questions you may get with short answers.
Longer answers are in [guion.md](guion.md).

## 1. The project in two minutes (read this first)

We built a **simulated cake factory** as a software system. No real machines: every machine is a program that pretends.

- **Six systems**, and each system is made of stations (a station = one program in its own Docker container):

| System | What it does in the factory | Stations (containers) |
|---|---|---|
| PLM (Production and Logistics Management) | Plans orders, knows the recipes, keeps the warehouse stock, ships the pallets | PLM-WH, PLM-SHIP |
| BAT (Batter Preparation) | Doses the ingredients, mixes, inspects the batter | BAT-DOSE, BAT-MIX, BAT-INSP |
| THM (Thermal Processing) | Puts batter in pans (depositing), bakes in a tunnel, checks, cools | THM-DEP, THM-BAKE, THM-EXIT, THM-COOL |
| FIN (Finishing and Packaging) | Takes the cake out of the pan, checks quality, wraps, packs on pallets, washes the pans | FIN-DPN, FIN-QUA, FIN-WRAP, FIN-PAL, FIN-WASH |
| BT (Band Transport) | The conveyor bands that carry the pans between stations | BT |
| MON (Line Monitoring and Health) | Watches all the others, stops the band if needed, opens tickets for Maintenance | MON |

- That is **16 factory containers** (one per station). Besides them there are: an **MQTT broker** (Mosquitto, the "post office"
  that all messages go through), **SQL Server** (orders, recipes, stock, event log), **MongoDB** (one document per pan) and the
  **dashboard** (the UI, port 8085). Everything runs with Docker Compose on one machine.
- **How the systems talk:** they never call each other directly. Every message goes through the broker, in a standard format called
  **Sparkplug B** (on top of MQTT). There are **14 signals** between the systems, for example "Batter ready" (BAT to THM) or
  "Pallet ready for shipment" (FIN to PLM).
- **The idea that matters for our talk, "birth and death certificates":** when a station connects it sends a *birth certificate*
  (NBIRTH: "I am online"). When it connects it also leaves a *death certificate* with the broker (the MQTT "Last Will", NDEATH). If the
  station dies, the broker publishes that certificate for it. So the monitor MON learns that a container died without anyone
  checking. This is our **failure detector**.
- **Recovery:** Docker restarts the dead container (about 0.8 s). The station reads its saved state from the database
  and sends a new birth certificate. Nothing is lost because the state (queues, pans) is in SQL/MongoDB.
- **Our quality attribute is availability.** The requirements (THM-REQ-71, 72, 73): 99.5 % availability per month, a stopped system must
  be detected within 15 s, and be back in normal operation within 60 s.
- **What we did to check it:** we built it (slides 3 and 4), modelled it with Papyrus and UPPAAL (speaker B), and broke it on
  purpose in an experiment with 270 runs (speakers B and C).

Keep this one sentence in your head: **"If one container dies, how fast do we notice, how fast is it back, and do we lose cakes?"**

## 2. What you say (about 1:35)

**Slide 1 (10 s).**
"Good morning. We are group 5. Our cake factory runs in sixteen Docker containers. Our question is simple: what happens when one
container dies? How fast do we notice, how fast is it back, and do we lose cakes?"

**Slide 2 (20 s).**
"We chose availability as our main quality attribute, because the requirements give numbers for it: 99.5 % availability per month,
a stopped system detected within 15 seconds, and back in normal operation within 60 seconds. We checked it in three steps: we built
it, we modelled it with Papyrus and UPPAAL, and we broke it on purpose in an experiment."

**Slide 3 (35 s).**
"The implementation is a set of Go programs, one container per station, sixteen in total, plus the MQTT broker, SQL Server and
MongoDB. All signals between the systems go through the broker as Sparkplug B messages. SQL Server holds orders, recipes and
stock, and MongoDB holds one document per pan, so a pan can be found again after a crash.
The row at the bottom is the story of every experiment run: the process ends, the broker publishes the death certificate, the
monitor sees it, Docker restarts the container, the station reads its state back from the database and sends a birth
certificate. So birth and death certificates are our failure detector."

**Slide 4 (30 s).**
"This is the real control UI of the factory, a dashboard. The coloured chips show every system online or down and how often it
restarted. This animation is a real moment of the experiment: a container was crashed, the monitor saw its death certificate,
Docker restarted it, and the event log shows it back with its last saved state, within a second. In the log you also see the
depositing station discarding a batter that waited 25 seconds: that is a rule we checked with UPPAAL, and my colleague will show
it now. The panel on the right can stop, freeze or restart any part: that is exactly how the experiment injects its faults."

Then say the hand-over sentence from [guion.md](guion.md) and step aside for speaker B.

Tips: do not read the diagram on slide 3 line by line, just point to PLM, BAT, THM, FIN at the top and the broker bar in the middle.
On slide 4, press play on the GIF when you start speaking, so the crash happens while you talk.

## 3. Questions you may get (with short answers)

**What is the system, in one sentence?**
A simulated cake factory in Go: six systems on sixteen Docker containers that talk through an MQTT broker, with SQL Server and MongoDB for the data.

**Why availability?**
Because the requirements give numbers for it (99.5 %, 15 s, 60 s), so we can measure it and say pass or fail. It is also what our architecture
decisions serve: one container per station, certificates as failure detector, state saved in a database.

**Why one container per station?**
A failure then affects only one station, Docker restarts exactly that one, and in the experiment we can fail each station on its own.
The cost is more containers and more connections to the broker.

**What is MQTT? What is Sparkplug B?**
MQTT is a light publish/subscribe protocol: stations publish messages on topics and the broker delivers them to who subscribed.
Sparkplug B is a standard on top of MQTT for industrial systems: it defines topic names and the birth/death/data messages, so we
get the failure detection and the state without inventing our own protocol.

**What are the birth and death certificates?**
NBIRTH: a station says "I am online" when it connects. NDEATH: a message the station leaves with the broker when it connects (the MQTT
"Last Will"). If the connection breaks, the broker publishes it for the station. The monitor MON listens to them.

**How fast is a failure detected?**
A crash: in about 0.3 seconds, because the connection closes and the broker publishes the death certificate at once. A frozen
station: in 6 to 8 seconds, because the broker only notices when the keep-alive messages stop (limit 7.5 s). Both are inside the 15 s.

**What happens to the messages when a station is down?**
We use QoS 1 and persistent sessions: the broker keeps the messages for the station until it reconnects. Some stations also save
their own queue in the database and read it back when they start.

**Why SQL and MongoDB, why both?**
SQL Server for data with a fixed structure and relations (orders, recipes, stock, shipments, event log). MongoDB for the pans: each pan is one document
that changes during its life and is read as a whole.

**What is the single point of failure?**
The broker, SQL Server and MongoDB. In the experiment we only fail the factory containers. A broker cluster would be the next step.

**What does the dashboard show and how do you inject faults?**
The state of every system (online or down, restarts), warnings and the event log, maintenance tickets, and the pans and cakes moving through
the line. The components panel can stop, freeze or restart a part. The experiment runner uses the dashboard's API to do the same.

**What is in the GIF?**
A recording of our own dashboard while the experiment crashes a container (BAT-INSP): the warning, MON seeing the death certificate,
Docker restarting it, and the depositing station discarding a batter that waited 25 s. It is a real run.

**Is it a real factory? Are the numbers realistic?**
No, the machines are simulated and the process times are compressed. Detection and recovery times depend only on our software and the
broker, so they would be similar in a real line. The failure rates are our choice, not measured.

**What are the requirements THM-REQ-71, 72, 73?**
71: 99.5 % monthly availability of the controllers. 72: a stopped controller is detected within 3 heartbeat intervals (15 s with a 5 s heartbeat).
73: back in normal operation within 60 s after the cause is removed.

**What is Docker Compose doing?**
It starts all the containers (the 16 stations, broker, databases, dashboard) together and restarts the ones that die.

If you get a question you do not know: say "[colleague] can answer that" or "we did not check that; what we know is ..., and to find
out we would ...". It is fine, they mostly want to see that you understand the limits.

## 4. Words you may need

| Word | Meaning |
|---|---|
| Container | One program running in its own isolated box (Docker). In our factory, one station. |
| Broker | The server that receives all messages and delivers them (Mosquitto). |
| QoS 1 | The broker delivers the message at least once, even if the receiver is offline for a while. |
| Persistent session | The broker remembers a client between connections and keeps its messages. |
| Heartbeat | A "still alive" message each station sends every 5 s. |
| Keep-alive | The MQTT ping; if it stops, the broker closes the connection (after 7.5 s for us). |
| Last Will | A message the client gives the broker on connect; the broker sends it if the client disappears. |
| Pan | The tray that carries the batter and the cake around the line; 96 pans circulate. |
| Outage | The time from the fault until the next cake ships (about 25 s, the line has to refill). |
