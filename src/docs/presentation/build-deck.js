// Builds docs/presentation/cake-factory-5min.pptx (5-minute talk, exercises 8 and 9 + implementation + models).
// Run:  NODE_PATH=<dir with pptxgenjs> node build-deck.js
// Numbers come from docs/experiment.md (results) and docs/loadtest-results.csv; update the DATA block when the data changes.
const pptxgen = require("pptxgenjs");
const path = require("path");
const fs = require("fs");
const img = (f) => path.join(__dirname, "img", f);

// ---------- data ----------
const DATA = {
  runs: 270, passed: 269,
  detectCrash: 0.33, detectCrashMax: 0.43,
  recoverMax: 1.19,
  detectFreeze1: 7.01, detectFreeze2: 6.55, detectFreeze3: 6.06,
  outage: 25, availability: "99.997", cakesPerHour: 3444,
  load: { nodes: ["16", "160", "800", "1600"], p50: [1.94, 1.71, 1.47, 6.91], p95: [22.98, 34.53, 36.66, 26.7], p99: [44.17, 41.71, 42.43, 35.9] },
};

// ---------- theme ----------
const C_ESP = "222222", C_CARA = "1F5FA8", C_CREAM = "FFFFFF", C_TEAL = "1F5FA8", C_SAND = "D0D0D0", C_INK = "222222", C_MUTE = "666666";
const pres = new pptxgen();
pres.layout = "LAYOUT_16x9"; // 10 x 5.625 in
pres.title = "ASAAT-cake-factory: surviving a failed container";
pres.author = "Group05";
const HEAD = "Calibri", BODY = "Calibri";

function base(slide, title, n) {
  slide.background = { color: C_CREAM };
  slide.addText(title, { x: 0.4, y: 0.25, w: 9.2, h: 0.5, fontFace: HEAD, fontSize: 26, bold: true, color: C_INK, margin: 0, isTextBox: true, objectName: "title" });
  slide.addText("Group05  |  ASAAT", { x: 0.4, y: 5.25, w: 3, h: 0.25, fontFace: BODY, fontSize: 9, color: C_MUTE, margin: 0, isTextBox: true });
  slide.addText(String(n), { x: 9.2, y: 5.25, w: 0.4, h: 0.25, fontFace: BODY, fontSize: 10, color: C_MUTE, align: "right", margin: 0, isTextBox: true, objectName: "page" });
}
const card = () => {}; // no boxes: plain slides
const txt = (slide, t, o) => slide.addText(t, Object.assign({ fontFace: BODY, fontSize: 12, color: C_INK, margin: 0, isTextBox: true, valign: "top" }, o));
const bullets = (arr, o = {}) => arr.map((t, i) => ({ text: t, options: Object.assign({ bullet: { indent: 14 }, breakLine: i < arr.length - 1, paraSpaceAfter: 5 }, o) }));
const stat = (slide, x, y, w, big, small, color = C_TEAL) => {
  txt(slide, big, { x, y, w, h: 0.5, fontFace: HEAD, fontSize: 26, bold: true, color });
  txt(slide, small, { x, y: y + 0.5, w, h: 0.4, fontSize: 11, color: C_MUTE });
};

// ---------- 1 title ----------
let s = pres.addSlide();
s.background = { color: "FFFFFF" };
txt(s, "ASAAT, SDU  |  Group05  |  ASAAT-cake-factory", { x: 0.8, y: 1.0, w: 8, h: 0.3, fontSize: 13, color: C_MUTE });
txt(s, "What happens when a container of the cake factory dies?", { x: 0.8, y: 1.6, w: 8.4, h: 1.6, fontFace: HEAD, fontSize: 38, bold: true, color: C_INK });
txt(s, "Implementation, UPPAAL model and an availability experiment with 270 runs", { x: 0.8, y: 3.5, w: 8, h: 0.4, fontSize: 18, color: C_INK });
s.addNotes("Hook: a factory line that stops is lost money. Our question: if one container dies, how fast do we notice, how fast is it back, and do we lose cakes? Full script in guion.md.");

// ---------- 2 goal ----------
s = pres.addSlide(); base(s, "Goal: availability, with numbers from the requirements", 2);
stat(s, 0.5, 1.05, 2.9, "99.5 %", "monthly availability (THM-REQ-71)");
stat(s, 3.55, 1.05, 2.9, "15 s", "stopped system detected (THM-REQ-72, 3 x T_hb)");
stat(s, 6.6, 1.05, 2.9, "60 s", "back in normal operation (THM-REQ-73)");
card(s, 0.5, 2.25, 9.0, 2.7);
txt(s, "How we check it", { x: 0.75, y: 2.4, w: 4, h: 0.3, fontFace: HEAD, fontSize: 16, bold: true, color: C_CARA });
const steps = [["1", "Build it", "Go stations, one Docker container each, Sparkplug B over MQTT"], ["2", "Model it", "Papyrus (structure, behaviour) and UPPAAL (timing, 50 queries)"], ["3", "Break it", "Experiment: inject 270 faults, measure detect, recover, outage, lost pans"]];
steps.forEach((st, i) => {
  const x = 0.75 + i * 2.95;
  txt(s, st[0] + "  " + st[1], { x, y: 2.95, w: 2.7, h: 0.4, fontFace: HEAD, fontSize: 16, bold: true, color: C_ESP });
  txt(s, st[2], { x, y: 3.45, w: 2.7, h: 1.0, fontSize: 12, color: C_INK });
});
txt(s, "Quality attribute: availability. Simulated line: 6 systems (PLM, BAT, THM, FIN, BT, MON), 16 factory containers.", { x: 0.75, y: 4.45, w: 8.5, h: 0.35, fontSize: 11, color: C_MUTE });
s.addNotes("Three numbers come from the THM requirements. We treat them as acceptance criteria for the experiment.");

// ---------- 3 implementation ----------
s = pres.addSlide(); base(s, "Implementation: every signal goes through the broker", 3);
s.addImage({ path: img("message-flow-crop.png"), x: 0.4, y: 1.0, w: 6.3, h: 2.415, objectName: "message-flow", altText: "Message flow between PLM, BAT, THM, FIN, MON and BT through the MQTT broker" });
card(s, 6.9, 0.95, 2.7, 2.95);
txt(s, bullets([
  "Go, one container per station (16) plus broker, SQL Server, MongoDB",
  "Sparkplug B on MQTT: QoS 1, persistent sessions",
  "Birth and death certificates are the failure detector",
  "SQL: orders, recipes, stock. MongoDB: one document per pan",
]), { x: 7.02, y: 1.07, w: 2.5, h: 2.8, fontSize: 11.5 });
txt(s, "Figure: message flow between the systems, all through the broker", { x: 0.4, y: 3.5, w: 6.3, h: 0.25, fontSize: 10, color: C_MUTE });
txt(s, "When a container dies", { x: 0.5, y: 4.07, w: 4, h: 0.25, fontFace: HEAD, fontSize: 12, bold: true });
const chain = [["Process ends", "Last Will: broker publishes death certificate"], ["MON notices", "event in the log, dashboard shows it"], ["Docker restarts", "restart policy, about 0.8 s"], ["State restored", "queue and pans read back from the DB"], ["Birth certificate", "system online again, line refills"]];
chain.forEach((c, i) => {
  const x = 0.5 + i * 1.82;
  s.addShape(pres.ShapeType.rect, { x, y: 4.35, w: 1.7, h: 0.85, fill: { color: "FFFFFF" }, line: { color: C_SAND, width: 1 }, objectName: "chain" });
  txt(s, [{ text: c[0], options: { bold: true, fontSize: 11, color: C_CARA, breakLine: true } }, { text: c[1], options: { fontSize: 9, color: C_INK } }], { x: x + 0.07, y: 4.39, w: 1.56, h: 0.78 });
});
s.addNotes("Show the picture: four production systems exchange the 14 signals, MON and BT sit beside. Then the chain at the bottom is the story of every experiment run.");


// ---------- 3b UI ----------
s = pres.addSlide(); base(s, "Implementation: the control UI the overseer sees", 4);
s.addImage({ path: img(fs.existsSync(path.join(__dirname,"img","ui-crash.gif")) ? "ui-crash.gif" : "ui-dashboard.png"), x: 0.4, y: 0.95, w: 6.3, h: 3.39, objectName: "ui", altText: "Animated dashboard of the cake factory during a crash of BAT-INSP" });
card(s, 6.9, 0.95, 2.7, 4.2);
txt(s, [{ text: "Dashboard on port 8085", options: { fontFace: HEAD, bold: true, fontSize: 14, color: C_CARA, breakLine: true } }].concat(bullets([
  "Live line: every system online or down, with its restart count",
  "Warnings: death certificate seen by MON, Docker restart, birth certificate back",
  "Event log stored in SQL, maintenance tickets for faults that need people",
  "Stop, freeze or restart any part: this is how the experiment injects its faults",
])), { x: 7.02, y: 1.07, w: 2.5, h: 4.0, fontSize: 11 });
txt(s, "Recording of the live UI while the experiment crashes BAT-INSP: warning, restart, and the batter in the queue discarded by THM-DEP after 25 s (the UPPAAL rule).", { x: 0.4, y: 4.45, w: 6.3, h: 0.6, fontSize: 11, color: C_MUTE });
s.addNotes("Show the real UI. The warnings are the injected crash and the death certificate of BAT-INSP; the log shows the container back with its last state loaded, and the batter that waited 25 s in the depositing queue discarded, which is the UPPAAL finding. Everything in the experiment is measured through this dashboard's API.");

// ---------- 5 uppaal ----------
s = pres.addSlide(); base(s, "UPPAAL: the timing model, and its simulation", 5);
s.addImage({ path: img("uppaal-trace.gif"), x: 0.4, y: 0.95, w: 6.2, h: 3.49, objectName: "uppaal-gif", altText: "Animated UPPAAL witness trace: a queued batter expires" });
txt(s, "Real witness trace of UPPAAL (verifyta, query E<> Dep.Expired), played step by step. Depositing stops, the batter waits, and after 25 s it must be discarded.", { x: 0.4, y: 4.5, w: 6.2, h: 0.6, fontSize: 10.5, color: C_MUTE });
stat(s, 6.8, 0.95, 2.8, "50 queries", "5 models, all satisfied", C_TEAL);
card(s, 6.8, 2.05, 2.8, 3.1);
txt(s, [{ text: "What it found", options: { fontFace: HEAD, bold: true, fontSize: 14, color: C_CARA, breakLine: true } }].concat(bullets([
  "In the first design a batter could wait 108 s, over the 25 s limit. Fix: THM-DEP discards it (Expired), now in code and tests",
  "Margins by bisection: Docker restart up to 43 s (code assumes 30)",
])), { x: 6.92, y: 2.15, w: 2.6, h: 2.95, fontSize: 11 });
s.addNotes("Play the GIF: each step is one transition of the real UPPAAL trace. Stress: UPPAAL is not a drawing, it found the 108 s wait that the experiment would only see by luck.");

// ---------- Papyrus ----------
s = pres.addSlide(); base(s, "Papyrus: structure and behaviour match the code", 6);
s.addImage({ path: img("plm-papyrus.png"), x: 0.4, y: 0.95, w: 3.5, h: 4.2, objectName: "papyrus-plm", altText: "Papyrus state machine of PLM" });
s.addImage({ path: img("papyrus-ibd.png"), x: 4.15, y: 0.95, w: 5.45, h: 2.43, objectName: "papyrus-ibd", altText: "Papyrus internal block diagram of the cake factory" });
txt(s, "Internal block diagram: 6 systems, ports for the 14 signals. Left: state machine of PLM.", { x: 4.15, y: 3.42, w: 5.45, h: 0.3, fontSize: 10.5, color: C_MUTE });
card(s, 4.15, 3.85, 5.45, 1.3);
txt(s, bullets([
  "One state machine per system, checked against the code and the other diagrams",
  "When model and code disagreed, we changed the code: PLM confirms BAT after the withdrawal, timer transition after 22 s",
]), { x: 4.28, y: 3.93, w: 5.2, h: 1.2, fontSize: 11.5 });
s.addNotes("Quick slide, 15 seconds. The IBD is the structure, the state machine is the behaviour.");

// ---------- 6 experiment design ----------
s = pres.addSlide(); base(s, "Exercise 8: experiment design", 7);
card(s, 0.5, 0.95, 4.4, 2.15);
txt(s, [{ text: "Goal (GQM)", options: { fontFace: HEAD, bold: true, fontSize: 14, color: C_CARA, breakLine: true } }, { text: "Analyze the Docker-deployed Sparkplug B factory for the purpose of evaluating availability from the point of view of the production overseer.", options: { fontSize: 14 } }], { x: 0.65, y: 1.05, w: 4.1, h: 2.0 });
card(s, 5.1, 0.95, 4.4, 2.15);
txt(s, [{ text: "Metrics", options: { fontFace: HEAD, bold: true, fontSize: 14, color: C_CARA, breakLine: true } }].concat(bullets([
  "detect_s: fault until death certificate (limit 15 s)",
  "recover_s: fault until birth certificate (limit 60 s)",
  "outage_s: fault until the next cake ships",
  "pans_lost: pans in the loop, before and after (limit 0)",
])), { x: 5.25, y: 1.05, w: 4.1, h: 2.0, fontSize: 12.5 });
stat(s, 0.5, 3.3, 1.9, "18", "scenarios", C_CARA);
stat(s, 2.55, 3.3, 1.9, "15", "runs each", C_CARA);
stat(s, 4.6, 3.3, 1.9, "270", "runs in total", C_CARA);
card(s, 6.65, 3.3, 2.85, 0.95);
txt(s, "15 crashes (14 stations + BT), 1 freeze, 2 multi-freezes", { x: 6.78, y: 3.38, w: 2.6, h: 0.8, fontSize: 11, valign: "middle" });
txt(s, "Per run: wait until settled, inject through the dashboard API, poll every 100 ms, measure, wait until settled. Then a 2 h availability run with doubled failure rates.", { x: 0.5, y: 4.45, w: 9.0, h: 0.6, fontSize: 12.5, color: C_INK });
s.addNotes("Crash and freeze are different mechanisms: a crash closes the connection and the broker reacts at once, a freeze is only seen when keep-alives stop. Fixed: heartbeat 5 s, real time, one recipe, no random failures.");

// ---------- 7 results ----------
s = pres.addSlide(); base(s, "Exercise 9: results of the 270 runs", 8);
s.addChart(pres.charts.BAR, [{ name: "Mean time to detect (s)", labels: ["Crash (15 containers)", "Freeze, 1 station", "Freeze, 2 stations", "Freeze, 3 stations"], values: [DATA.detectCrash, DATA.detectFreeze1, DATA.detectFreeze2, DATA.detectFreeze3] }], {
  x: 0.4, y: 0.9, w: 5.1, h: 3.15, barDir: "col", chartColors: [C_TEAL], showTitle: true, title: "Detection time in s (requirement: 15 s)", titleFontFace: BODY, titleFontSize: 12, titleColor: C_INK,
  showValue: true, dataLabelPosition: "outEnd", dataLabelFontSize: 11, dataLabelFormatCode: "0.0", dataLabelColor: C_INK, dataLabelFontFace: "+mn-lt",
  showLegend: false, catAxisLabelColor: C_INK, catAxisLabelFontSize: 10, catAxisLabelFontFace: "+mn-lt", valAxisLabelColor: C_MUTE, valAxisLabelFontSize: 10, valAxisLabelFontFace: "+mn-lt", valAxisMaxVal: 16, valAxisMinVal: 0, valAxisMajorUnit: 4,
  valGridLine: { color: C_SAND, size: 0.75 }, catGridLine: { style: "none" }, barGapWidthPct: 60,
});
stat(s, 5.75, 0.95, 1.85, `${DATA.passed}/${DATA.runs}`, "runs passed");
stat(s, 7.7, 0.95, 1.85, `${DATA.recoverMax.toFixed(1)} s`, "slowest recovery (limit 60 s)");
stat(s, 5.75, 2.0, 1.85, "0", "pans lost", C_CARA);
stat(s, 7.7, 2.0, 1.85, `${DATA.availability} %`, "availability, 2 h run", C_CARA);
card(s, 5.75, 3.05, 3.8, 1.0);
txt(s, `${DATA.cakesPerHour} cakes/h during the 2 h run with doubled failure rates (96 % of maximum 3600).`, { x: 5.88, y: 3.12, w: 3.55, h: 0.85, fontSize: 11.5, valign: "middle" });
card(s, 0.4, 4.25, 9.2, 0.95);
txt(s, bullets([
  `A crash is found in 0.3 s, a freeze in 6 to 7 s (broker keep-alive limit 7.5 s). Both are far inside 15 s.`,
  `The production outage is about ${DATA.outage} s for almost every container: the line has to refill. Detection and recovery are under 1.2 s, so refill, not failure handling, dominates availability.`,
]), { x: 0.55, y: 4.3, w: 8.9, h: 0.85, fontSize: 11 });
s.addNotes("One run failed: a pan count taken while a pan was between two stations, looks like a measurement artefact. All requirements are met in this setup. Threats: simulated machines, one host, chosen failure rates.");

// ---------- 8 scalability + conclusion ----------
s = pres.addSlide(); base(s, "Scalability and conclusion", 9);
s.addChart(pres.charts.LINE, [
  { name: "median", labels: DATA.load.nodes, values: DATA.load.p50 },
  { name: "95th percentile", labels: DATA.load.nodes, values: DATA.load.p95 },
  { name: "99th percentile", labels: DATA.load.nodes, values: DATA.load.p99 },
], {
  x: 0.4, y: 0.9, w: 4.9, h: 3.0, chartColors: [C_CARA, "8C8C8C", C_ESP], lineSize: 2.5, lineDataSymbolSize: 7, showTitle: true, title: "Broker delay in ms, simulated nodes (0 % loss)", titleFontFace: BODY, titleFontSize: 12, titleColor: C_INK,
  showLegend: true, legendPos: "b", legendFontSize: 10, legendColor: C_INK, legendFontFace: "+mn-lt", catAxisLabelColor: C_INK, catAxisLabelFontSize: 10, catAxisLabelFontFace: "+mn-lt", valAxisLabelColor: C_MUTE, valAxisLabelFontSize: 10, valAxisLabelFontFace: "+mn-lt",
  valAxisMinVal: 0, valAxisMaxVal: 60, valAxisMajorUnit: 20, valGridLine: { color: C_SAND, size: 0.75 }, catGridLine: { style: "none" }, showCatAxisTitle: true, catAxisTitle: "simulated nodes", catAxisTitleFontSize: 10, catAxisTitleColor: C_MUTE,
});
txt(s, "Load test: 16 to 1600 nodes, heartbeat every 5 s, 50 handoffs/s. The broker is not the first limit; availability is.", { x: 0.4, y: 3.95, w: 4.9, h: 0.5, fontSize: 10.5, color: C_MUTE });
card(s, 5.55, 0.95, 4.0, 3.5, C_ESP);
txt(s, [{ text: "Conclusion", options: { fontFace: HEAD, bold: true, fontSize: 16, color: C_CARA, breakLine: true } }].concat(bullets([
  "Requirements met: detect 15 s, recover 60 s, no pan lost, 99.5 %",
  "Model, code and test agree: UPPAAL found a real defect, the experiment found another (queue restore) that we fixed",
  "Limits: simulated machines, one host, chosen failure rates, 15 runs per scenario",
  "Next: real network, a broker cluster, measured failure rates",
])), { x: 5.72, y: 1.07, w: 3.7, h: 3.3, fontSize: 13, color: C_INK });
txt(s, "Questions?", { x: 0.4, y: 4.7, w: 5, h: 0.4, fontFace: HEAD, fontSize: 20, bold: true, color: C_TEAL });
s.addNotes("Finish with the one-sentence answer: a dead container is noticed in under a second, back in about a second, no cake lost; the line needs about 25 s to refill.");

// ---------- appendix ----------
s = pres.addSlide(); base(s, "Appendix: data model (SQL Server)", 10);
s.addImage({ path: img("er-diagram.png"), x: 0.4, y: 0.95, w: 9.2, h: 3.81, objectName: "er", altText: "ER diagram of the SQL Server tables" });
txt(s, "Solid line: declared foreign key. Dashed: reference by id, not enforced. Pans live in MongoDB, one document each.", { x: 0.4, y: 4.85, w: 9.2, h: 0.3, fontSize: 11, color: C_MUTE });

s = pres.addSlide(); base(s, "Appendix: UPPAAL templates", 11);
s.addImage({ path: img("batter-dep.png"), x: 0.4, y: 0.95, w: 5.5, h: 3.54, objectName: "uppaal-batter", altText: "UPPAAL template of the depositing station" });
txt(s, "Depositing station (THM-DEP)", { x: 0.4, y: 4.55, w: 5.5, h: 0.3, fontSize: 11, color: C_MUTE });
s.addImage({ path: img("supervision-node.png"), x: 6.2, y: 1.0, w: 3.3, h: 2.19, objectName: "uppaal-node", altText: "UPPAAL template of a node" });
txt(s, "A node: Online, Down, Starting", { x: 6.2, y: 3.25, w: 3.3, h: 0.3, fontSize: 11, color: C_MUTE });

s = pres.addSlide(); base(s, "Appendix: how much room do the design values have?", 12);
const H = (t) => ({ text: t, options: { bold: true, color: C_INK, fill: { color: "E6E6E6" }, fontSize: 11 } });
const R = (a, b, c, d, e) => [a, b, c, d, e].map((t, i) => ({ text: String(t), options: { fontSize: 10.5, color: C_INK, align: i ? "center" : "left", fill: { color: "FFFFFF" } } }));
s.addTable([
  [H("Experiment"), H("Parameter"), H("Used"), H("UPPAAL accepts"), H("Margin")],
  R("How slow may a Docker restart be?", "RDMAX", "30 s", "up to 43 s", "13 s"),
  R("How late may the broker declare a node dead?", "DEADAFTER", "8 s", "up to 15 s", "7 s"),
  R("Smallest safe batch-lost limit of dosing", "LOST", "120 s", "from 44 s", "76 s"),
  R("Largest batter wait", "T_batter", "25 s", "up to 102 s", "77 s"),
  R("Longest stop of depositing", "BLOCKMAX", "60 s", "up to 54 s", "-6 s (explained)"),
  R("Largest restock time of PLM", "RESTOCK", "15 s", "up to 17 s", "2 s"),
  R("Smallest BAT retry time", "RETRY", "20 s", "from 18 s", "2 s"),
  R("Largest reconcile period", "RECONCILE", "5 s", "up to 5 s", "0 s"),
], { x: 0.5, y: 1.0, w: 9.0, colW: [3.7, 1.4, 1.0, 1.7, 1.2], rowH: 0.36, border: { type: "solid", pt: 0.75, color: C_SAND }, fontFace: BODY, valign: "middle" });
txt(s, "Negative margin on BLOCKMAX is not a failure: at the estimated 60 s the late discard comes after dosing gave up, so nothing is deposited from that batch.", { x: 0.5, y: 4.55, w: 9.0, h: 0.5, fontSize: 11, color: C_MUTE });

pres.writeFile({ fileName: path.join(__dirname, "cake-factory-5min.pptx") }).then((f) => console.log("written", f));
