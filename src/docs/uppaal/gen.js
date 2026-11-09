// Generates the UPPAAL models (*.xml) and query files (*.q) of docs/timed-automata.md.
// Run: node gen.js   Check: verifyta -q <model>.xml <model>.q
const fs = require("fs");
const esc = (s) => s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");

function template(t) {
  const n = t.locs.length;
  const pos = (i) => t.locs[i].at || [Math.round(300 * Math.cos((2 * Math.PI * i) / n)), Math.round(200 * Math.sin((2 * Math.PI * i) / n))]; // `at` places a location by hand
  let s = `<template>\n<name>${t.name}</name>\n`;
  if (t.params) s += `<parameter>${esc(t.params)}</parameter>\n`;
  s += `<declaration>${esc(t.decl || "")}</declaration>\n`;
  t.locs.forEach((l, i) => {
    const [x, y] = pos(i);
    s += `<location id="${t.name}_${l.n}" x="${x}" y="${y}"><name x="${x - 20}" y="${y + 18}">${l.n}</name>`;
    if (l.inv) s += `<label kind="invariant" x="${x - 20}" y="${y + 36}">${esc(l.inv)}</label>`;
    if (l.committed) s += `<committed/>`;
    s += `</location>\n`;
  });
  s += `<init ref="${t.name}_${t.init}"/>\n`;
  const pairKey = (e) => [e.from, e.to].sort().join("|");
  const groups = {};
  t.trans.forEach((e) => { (groups[pairKey(e)] = groups[pairKey(e)] || []).push(e); });
  t.trans.forEach((e) => {
    s += `<transition><source ref="${t.name}_${e.from}"/><target ref="${t.name}_${e.to}"/>`;
    const g = groups[pairKey(e)], k = g.indexOf(e), m = g.length;
    const a = pos(t.locs.findIndex((l) => l.n === e.from)), b = pos(t.locs.findIndex((l) => l.n === e.to));
    let lx, ly, nails = "";
    if (e.from === e.to) {
      const h = 55 + k * 85;
      nails = `<nail x="${a[0] + 45}" y="${a[1] - h}"/><nail x="${a[0] - 45}" y="${a[1] - h}"/>`;
      lx = a[0] + 55; ly = a[1] - h - 20;
    } else {
      // perpendicular of the canonical direction, so edges in opposite directions bend to opposite sides
      const fwd = e.from < e.to, dx = fwd ? b[0] - a[0] : a[0] - b[0], dy = fwd ? b[1] - a[1] : a[1] - b[1], len = Math.hypot(dx, dy) || 1;
      const off = (k - (m - 1) / 2) * 120;
      const nx = Math.round((a[0] + b[0]) / 2 - (dy / len) * off), ny = Math.round((a[1] + b[1]) / 2 + (dx / len) * off);
      if (m > 1) nails = `<nail x="${nx}" y="${ny}"/>`;
      lx = nx + 8; ly = ny - 30;
    }
    const lines = [e.select, e.guard, e.sync, e.assign].filter(Boolean);
    const w = 8 * Math.max(...lines.map((x) => x.length), 1);
    if (e.from !== e.to) {
      const flat = Math.abs(b[0] - a[0]) > 1.5 * Math.abs(b[1] - a[1]);
      if (m > 1) { lx = lx - 8; ly = ly + 30 - 8; }
      if (flat) { lx = Math.round((lx + (m > 1 ? 0 : -10)) - (m > 1 ? w / 2 : w / 2)); ly = ly + 30 - 15 * lines.length - 12; }
      else if (m === 1) {
        // steep single edge: put the label beside the line, on the side away from the centre of the layout
        const mxm = (a[0] + b[0]) / 2, mym = (a[1] + b[1]) / 2, l = Math.hypot(mxm, mym) || 1;
        const ox = mxm / l, oy = mym / l;
        lx = Math.round(mxm + ox * 30 - (ox < 0 ? w : 0));
        ly = Math.round(mym + oy * 30 - (oy < 0 ? 16 * lines.length : 0));
      }
    }
    let ky = ly;
    const put = (kind, v) => { if (v) { s += `<label kind="${kind}" x="${lx}" y="${ky}">${esc(v)}</label>`; ky += 16; } };
    put("select", e.select); put("guard", e.guard); put("synchronisation", e.sync); put("assignment", e.assign);
    s += nails + `</transition>
`;
  });
  return s + `</template>\n`;
}

const TICK = { name: "Tick", init: "On", locs: [{ n: "On" }], trans: [{ from: "On", to: "On" }] }; // keeps `not deadlock` meaningful: a finished run may idle
function model(file, decl, templates, system, queries) {
  if (file !== "supervision") { templates = templates.concat([TICK]); system = system.replace(";", ", Tick;"); }
  const xml = `<?xml version="1.0" encoding="utf-8"?>\n<!DOCTYPE nta PUBLIC '-//Uppaal Team//DTD Flat System 1.1//EN' 'http://www.it.uu.se/research/group/darts/uppaal/flat-1_1.dtd'>\n<nta>\n<declaration>${esc(decl)}</declaration>\n${templates.map(template).join("")}<system>${esc(system)}</system>\n</nta>\n`;
  fs.writeFileSync(file + ".xml", xml);
  fs.writeFileSync(file + ".q", queries.map((q) => `//${q[0]}\n${q[1]}\n`).join("\n"));
}

// 1. Supervision: node, broker (death certificate), MON, Docker, fault environment.
model("supervision", `// seconds; estimated where the plant value is unknown
const int HB = 5;          // heartbeat T_hb
const int DEADAFTER = 8;   // broker: 1.5 x keep-alive 5 s = 7.5 s, rounded up
const int DETECT = 15;     // THM-REQ-72
const int RESTORE = 60;    // THM-REQ-73
const int ESCALATE = 45;   // MON escalates after this
const int RDMIN = 1;       // Docker restart takes at least this
const int RDMAX = 30;      // estimated upper bound of a restart
clock t;                   // observer: time since the fault
chan hb, restart;
broadcast chan crash, freeze, birth, ndeath, escalate;
`, [
  { name: "Node", decl: "clock x;", init: "Online",
    locs: [{ n: "Starting", inv: "x <= 1" }, { n: "Online", inv: "x <= HB" }, { n: "Down" }],
    trans: [
      { from: "Starting", to: "Online", sync: "birth!" },
      { from: "Online", to: "Online", guard: "x >= HB", sync: "hb!", assign: "x = 0" },
      { from: "Online", to: "Down", sync: "crash?" },
      { from: "Online", to: "Down", sync: "freeze?" },
      { from: "Down", to: "Starting", sync: "restart?", assign: "x = 0" } ] },
  { name: "Broker", decl: "clock y;", init: "Alive",
    locs: [{ n: "Alive", inv: "y <= DEADAFTER" }, { n: "Notify", committed: true }, { n: "Dead" }],
    trans: [
      { from: "Alive", to: "Alive", sync: "hb?", assign: "y = 0" },
      { from: "Alive", to: "Alive", sync: "birth?", assign: "y = 0" }, // a (re)connection is traffic too
      { from: "Alive", to: "Notify", sync: "crash?" },
      { from: "Notify", to: "Dead", sync: "ndeath!" },
      { from: "Alive", to: "Dead", guard: "y >= DEADAFTER", sync: "ndeath!" },
      { from: "Dead", to: "Alive", sync: "birth?", assign: "y = 0" } ] },
  { name: "MON", decl: "clock z;", init: "Watching",
    locs: [{ n: "Watching" }, { n: "Failed", inv: "z <= ESCALATE" }, { n: "Escalated" }],
    trans: [
      { from: "Watching", to: "Failed", sync: "ndeath?", assign: "z = 0" },
      { from: "Failed", to: "Watching", sync: "birth?" },
      { from: "Failed", to: "Escalated", guard: "z >= ESCALATE", sync: "escalate!" } ] },
  { name: "Docker", decl: "clock d;", init: "Idle",
    locs: [{ n: "Idle" }, { n: "Restarting", inv: "d <= RDMAX" }],
    trans: [
      { from: "Idle", to: "Restarting", sync: "crash?", assign: "d = 0" },
      { from: "Idle", to: "Restarting", sync: "freeze?", assign: "d = 0" },
      { from: "Restarting", to: "Idle", guard: "d >= RDMIN", sync: "restart!" } ] },
  { name: "Env", init: "Idle",
    locs: [{ n: "Idle" }, { n: "Faulted" }],
    trans: [
      { from: "Idle", to: "Faulted", sync: "crash!", assign: "t = 0" },
      { from: "Idle", to: "Faulted", sync: "freeze!", assign: "t = 0" } ] },
], "system Node, Broker, MON, Docker, Env;", [
  ["no deadlock", "A[] not deadlock"],
  ["T-RC1: a silent node is known to MON within DEADAFTER (<= DETECT)", "A[] (Node.Down && MON.Watching) imply t <= DEADAFTER"],
  ["T-RC1: and within the 15 s of THM-REQ-72", "A[] (Node.Down && MON.Watching) imply t <= DETECT"],
  ["T-RC2: node back within 60 s", "A[] (Env.Faulted && !Node.Online) imply t <= RESTORE"],
  ["MON never has to escalate", "A[] not MON.Escalated"],
  ["a fault is reachable", "E<> Node.Down"],
  ["recovery is reachable", "E<> (Env.Faulted && Node.Online)"],
  ["every failure ends in recovery (leads-to)", "Node.Down --> Node.Online"],
]);

// 2. Band: a halted section halts everything upstream; release only after the owner is back.
model("band", `const int N = 5;          // sections S1..S5, index 0 is the most upstream
const int HOP = 1;        // T-RC3: every section reacts within 1 s
const int RDMIN = 1, RDMAX = 30; // owner restart, estimated
typedef int[0,N-1] id_t;
chan halt[N];
broadcast chan stop[N+1];   // stop[N] is the sink of the most upstream section
broadcast chan release;
bool ownerUp[N] = {true,true,true,true,true};
bool faulted = false, released = false;
int hk = 0;               // the section whose owner failed
int nstopped = 0;
clock t;
`, [
  { name: "Section", params: "const id_t i", decl: "clock x;", init: "Running",
    locs: [{ n: "Running" }, { n: "Pass", inv: "x <= HOP" }, { n: "Stopped" }],
    trans: [
      { from: "Running", to: "Pass", sync: "halt[i]?", assign: "x = 0" },
      { from: "Running", to: "Pass", sync: "stop[i]?", assign: "x = 0" },
      { from: "Pass", to: "Stopped", sync: "stop[i>0?i-1:N]!", assign: "nstopped++" },
      { from: "Stopped", to: "Running", sync: "release?", assign: "nstopped--" } ] },
  { name: "Env", decl: "", init: "Idle",
    locs: [{ n: "Idle" }, { n: "Down", inv: "t <= RDMAX" }, { n: "Back" }],
    trans: [
      { from: "Idle", to: "Down", select: "k : id_t", sync: "halt[k]!", assign: "hk = k, ownerUp[k] = false, faulted = true, t = 0" },
      { from: "Down", to: "Back", guard: "t >= RDMIN", assign: "ownerUp[hk] = true" } ] },
  { name: "MON", init: "Idle",
    locs: [{ n: "Idle" }, { n: "Done" }],
    trans: [{ from: "Idle", to: "Done", guard: "faulted && ownerUp[hk] && nstopped == hk + 1", sync: "release!", assign: "released = true" }] },
], "system Section, Env, MON;", [
  ["no deadlock", "A[] not deadlock"],
  ["the halt spreads upstream within N hops of 1 s", "A[] (faulted && !released && t > N * HOP) imply nstopped == hk + 1"],
  ["a section passes the halt on within 1 s", "A[] forall (i : id_t) Section(i).Pass imply Section(i).x <= HOP"],
  ["never released before the owner is back", "A[] released imply ownerUp[hk]"],
  ["after the release nothing stays stopped", "A[] released imply nstopped == 0"],
  ["no more stopped sections than are upstream", "A[] nstopped <= hk + 1"],
  ["release is reachable", "E<> released"],
]);

// 3. Batter: inspection, hold while depositing is stopped (P3 = 25 s), batch lost limit (120 s).
// Models the code: BAT-INSP reads its own copy of the "Depositing stopped" state, holds the batter
// while the copy says stopped, discards it after P3, otherwise sends "Batter ready" and is done.
// There is no acknowledgement from depositing (decision: docs/team-review.md section 1, item 2, option B).
model("batter", `const int MIX = 6;       // recipe MixSeconds
const int INSP = 3;       // inspection
const int P3 = 25;        // batter waits at most this for depositing to work again (FC7)
const int POLL = 1;       // BAT-INSP looks at the state once a second, so it discards after P3 + POLL
const int LOST = 120;     // dosing gives up a batch that is not finished by now
const int STALE = 6;      // BAT-INSP's copy of the state is at most this old (heartbeat 5 s + delivery 1 s, estimated)
const int BLOCKMAX = 60;  // depositing may stay stopped this long (estimated)
const int DEPSTART = 24;  // a queued batter waits at most this for depositing to take it up: it may sit behind the previous batch, PansPerBatch 24 x depositTime 1 s
clock g;                  // age of the batch
clock h;                  // time since the inspector approved the batter
bool stopped = false;     // depositing is really stopped
bool seen = false;        // BAT-INSP's copy of that state
bool queued = false;      // "Batter ready" has reached depositing
bool faultDone = false;   // one stop only
bool deposited = false;
chan ready;               // Batter ready (reliable, no acknowledgement)
`, [
  { name: "Batch", decl: "clock x;", init: "Mixing",
    locs: [{ n: "Mixing", inv: "x <= MIX" }, { n: "Inspecting", inv: "x <= INSP" }, { n: "Handed" }, { n: "Holding", inv: "x <= P3 + POLL" }, { n: "Discarded" }],
    trans: [
      { from: "Mixing", to: "Inspecting", guard: "x >= MIX", assign: "x = 0" },
      { from: "Inspecting", to: "Holding", guard: "x >= INSP && seen", assign: "x = 0, h = 0" },
      { from: "Inspecting", to: "Handed", guard: "x >= INSP && !seen", sync: "ready!", assign: "h = 0" },
      { from: "Holding", to: "Handed", guard: "!seen", sync: "ready!" },
      { from: "Holding", to: "Discarded", guard: "seen && x >= P3 + POLL" } ] },
  { name: "Dep", decl: "clock y, q;", init: "Open",
    locs: [{ n: "Open", at: [0, -110] }, { n: "OpenQ", inv: "q <= DEPSTART", at: [0, 190] }, { n: "Blocked", inv: "y <= BLOCKMAX", at: [-380, 40] }, { n: "Took", committed: true, at: [380, 190] }, { n: "Done", at: [380, -110] }],
    trans: [
      { from: "Open", to: "OpenQ", sync: "ready?", assign: "queued = true, q = 0" },
      { from: "Blocked", to: "Blocked", sync: "ready?", assign: "queued = true" },
      { from: "Open", to: "Blocked", guard: "!faultDone", assign: "stopped = true, y = 0" },
      { from: "OpenQ", to: "Blocked", guard: "!faultDone", assign: "stopped = true, y = 0" },
      { from: "Blocked", to: "Open", guard: "y >= 1 && !queued", assign: "stopped = false, faultDone = true" },
      { from: "Blocked", to: "OpenQ", guard: "y >= 1 && queued", assign: "stopped = false, faultDone = true, q = 0" },
      { from: "OpenQ", to: "Took", assign: "deposited = true" },
      { from: "Took", to: "Done" } ] },
  { name: "Copy", decl: "clock w;", init: "Sync",
    locs: [{ n: "Sync", inv: "w <= STALE" }],
    trans: [{ from: "Sync", to: "Sync", guard: "w >= 1", assign: "seen = stopped, w = 0" }] },
], "system Batch, Dep, Copy;", [
  ["no deadlock", "A[] not deadlock"],
  ["T-RC4: before the handoff the batter holds at most P3 + POLL", "A[] Batch.Holding imply Batch.x <= P3 + POLL"],
  ["BAT's side of the batch is finished within MIX + INSP + P3 + POLL", "A[] (Batch.Mixing || Batch.Inspecting || Batch.Holding) imply g <= MIX + INSP + P3 + POLL"],
  ["T-RC6: and so well before the LOST limit", "A[] (Batch.Mixing || Batch.Inspecting || Batch.Holding) imply g <= LOST"],
  ["T-RC6: a batter is deposited before dosing gives the batch up", "A[] Dep.Took imply g <= LOST"],
  ["from approval to deposit at most DEPSTART + BLOCKMAX + DEPSTART (108 s), more than P3", "A[] Dep.Took imply h <= 2 * DEPSTART + BLOCKMAX"],
  ["deposited and discarded exclude each other", "A[] not (Batch.Discarded && deposited)"],
  ["the discard path is reachable", "E<> Batch.Discarded"],
  ["the deposit path is reachable", "E<> Dep.Took"],
  ["FINDING: the state copy can be stale, a batter is handed to depositing while it is stopped", "E<> (Batch.Handed && stopped)"],
  ["FINDING: after the handoff nothing limits the wait, a batter is deposited more than P3 + POLL after approval", "E<> (Dep.Took && h > P3 + POLL)"],
]);

// 4. Handover: document stored, signal may be lost, reconcile pass adopts after the grace.
model("handover", `const int GRACE = 3;      // station.HandoverGrace
const int RECONCILE = 5;  // station.ReconcileEvery
const int DELMAX = 1;     // message delivery, estimated
bool stored = false, adopted = false;
clock a;                  // time since the document was stored
chan handoff, deliver;
`, [
  { name: "Sender", init: "Idle",
    locs: [{ n: "Idle" }, { n: "Stored", committed: true }, { n: "Signalled" }],
    trans: [
      { from: "Idle", to: "Stored", assign: "stored = true, a = 0" },
      { from: "Stored", to: "Signalled", sync: "handoff!" } ] },
  { name: "Message", decl: "clock m;", init: "Idle",
    locs: [{ n: "Idle" }, { n: "Flight", inv: "m <= DELMAX" }, { n: "Lost" }, { n: "Delivered" }],
    trans: [
      { from: "Idle", to: "Flight", sync: "handoff?", assign: "m = 0" },
      { from: "Flight", to: "Delivered", sync: "deliver!" },
      { from: "Flight", to: "Lost" } ] },
  { name: "Receiver", decl: "clock r;", init: "Idle",
    locs: [{ n: "Idle", inv: "r <= RECONCILE" }, { n: "Adopted" }],
    trans: [
      { from: "Idle", to: "Adopted", sync: "deliver?", assign: "adopted = true" },
      { from: "Idle", to: "Adopted", guard: "stored && a >= GRACE && r >= RECONCILE", assign: "adopted = true" },
      { from: "Idle", to: "Idle", guard: "!stored && r >= RECONCILE", assign: "r = 0" },
      { from: "Idle", to: "Idle", guard: "stored && a < GRACE && r >= RECONCILE", assign: "r = 0" } ] },
], "system Sender, Message, Receiver;", [
  ["no deadlock", "A[] not deadlock"],
  ["T-RC7: a stored document is taken up within GRACE + RECONCILE", "A[] (stored && !adopted) imply a <= GRACE + RECONCILE"],
  ["a lost message is made up by the reconcile pass", "E<> (Message.Lost && Receiver.Adopted)"],
  ["a delivered message is adopted at once", "E<> (Message.Delivered && Receiver.Adopted)"],
]);
