// Generates the UPPAAL models (*.xml) and query files (*.q) of docs/timed-automata.md.
// Run: node gen.js   Check: verifyta -q <model>.xml <model>.q
const fs = require("fs");
const esc = (s) => s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");

// Labels are placed automatically: for every edge a set of candidate boxes beside the edge is scored
// against the boxes of the other labels, the circles and names of the locations and all edges, and the
// box with the fewest collisions (then the shortest distance to the edge) wins.
const CW = 8.4, LH = 16; // width of a character and height of a line in the UPPAAL editor
const overlap = (a, b) => a.x < b.x + b.w && b.x < a.x + a.w && a.y < b.y + b.h && b.y < a.y + a.h;
function segHitsRect(p, q, r) { // Liang-Barsky clipping of the segment pq against the rectangle r
  let t0 = 0, t1 = 1;
  const dx = q[0] - p[0], dy = q[1] - p[1];
  const clip = (pp, qq) => {
    if (pp === 0) return qq >= 0;
    const rr = qq / pp;
    if (pp < 0) { if (rr > t1) return false; if (rr > t0) t0 = rr; } else { if (rr < t0) return false; if (rr < t1) t1 = rr; }
    return true;
  };
  return clip(-dx, p[0] - r.x) && clip(dx, r.x + r.w - p[0]) && clip(-dy, p[1] - r.y) && clip(dy, r.y + r.h - p[1]);
}

function template(t) {
  const n = t.locs.length;
  const pos = (i) => t.locs[i].at || [Math.round(300 * Math.cos((2 * Math.PI * i) / n)), Math.round(200 * Math.sin((2 * Math.PI * i) / n))]; // `at` places a location by hand
  const idx = (name) => t.locs.findIndex((l) => l.n === name);
  let s = `<template>\n<name>${t.name}</name>\n`;
  if (t.params) s += `<parameter>${esc(t.params)}</parameter>\n`;
  s += `<declaration>${esc(t.decl || "")}</declaration>\n`;
  const fixed = []; // boxes that labels must keep away from: circles, names and invariants
  t.locs.forEach((l, i) => {
    const [x, y] = pos(i);
    s += `<location id="${t.name}_${l.n}" x="${x}" y="${y}"><name x="${x - 20}" y="${y + 18}">${l.n}</name>`;
    fixed.push({ x: x - 16, y: y - 16, w: 32, h: 32 }, { x: x - 22, y: y + 16, w: CW * l.n.length + 6, h: LH });
    if (l.inv) { s += `<label kind="invariant" x="${x - 20}" y="${y + 36}">${esc(l.inv)}</label>`; fixed.push({ x: x - 22, y: y + 33, w: CW * l.inv.length + 6, h: LH }); }
    if (l.committed) s += `<committed/>`;
    s += `</location>\n`;
  });
  s += `<init ref="${t.name}_${t.init}"/>\n`;

  // geometry of every edge: the polyline through its nails
  const pairKey = (e) => [e.from, e.to].sort().join("|");
  const groups = {};
  t.trans.forEach((e) => { (groups[pairKey(e)] = groups[pairKey(e)] || []).push(e); });
  const geo = t.trans.map((e) => {
    const g = groups[pairKey(e)], k = g.indexOf(e), m = g.length;
    const a = pos(idx(e.from)), b = pos(idx(e.to));
    let nails = [];
    if (e.via) {
      nails = e.via; // `via` bends an edge through the given points
    } else if (e.from === e.to) {
      const h = 55 + k * 85;
      nails = [[a[0] + 45, a[1] - h], [a[0] - 45, a[1] - h]];
    } else if (m > 1) {
      // perpendicular of the canonical direction, so edges in opposite directions bend to opposite sides
      const fwd = e.from < e.to, dx = fwd ? b[0] - a[0] : a[0] - b[0], dy = fwd ? b[1] - a[1] : a[1] - b[1], len = Math.hypot(dx, dy) || 1;
      const off = (k - (m - 1) / 2) * (t.spread || 120); // `spread` widens the bend of parallel edges for templates with long labels
      nails = [[Math.round((a[0] + b[0]) / 2 - (dy / len) * off), Math.round((a[1] + b[1]) / 2 + (dx / len) * off)]];
    }
    return { e, pts: [a, ...nails, b], nails };
  });
  const segs = [];
  geo.forEach((gx, i) => { for (let j = 0; j + 1 < gx.pts.length; j++) segs.push({ p: gx.pts[j], q: gx.pts[j + 1], edge: i }); });

  const placed = [];
  const order = geo.map((_, i) => i).sort((i, j) => labelLines(geo[j].e).length * 1000 + width(geo[j].e) - labelLines(geo[i].e).length * 1000 - width(geo[i].e));
  function labelLines(e) { return [e.select, e.guard, e.sync, e.assign].filter(Boolean); }
  function width(e) { return Math.max(0, ...labelLines(e).map((x) => x.length)) * CW; }
  const boxes = {};
  order.forEach((i) => {
    const gx = geo[i], lines = labelLines(gx.e);
    if (!lines.length) return;
    const w = Math.ceil(width(gx.e)) + 6, h = LH * lines.length;
    // cumulative length along the polyline
    const lens = [0];
    for (let j = 0; j + 1 < gx.pts.length; j++) lens.push(lens[j] + Math.hypot(gx.pts[j + 1][0] - gx.pts[j][0], gx.pts[j + 1][1] - gx.pts[j][1]));
    const total = lens[lens.length - 1] || 1;
    const at = (f) => {
      const d = f * total;
      let j = 0;
      while (j + 2 < lens.length && lens[j + 1] < d) j++;
      const p = gx.pts[j], q = gx.pts[j + 1], seg = lens[j + 1] - lens[j] || 1, u = (d - lens[j]) / seg;
      return { x: p[0] + (q[0] - p[0]) * u, y: p[1] + (q[1] - p[1]) * u, tx: (q[0] - p[0]) / seg, ty: (q[1] - p[1]) / seg };
    };
    let best = null;
    [0.5, 0.4, 0.6, 0.3, 0.7, 0.2, 0.8].forEach((f, fi) => {
      const P = at(f);
      [[-P.ty, P.tx], [P.ty, -P.tx]].forEach((nrm) => {
        [4, 16, 34, 58, 90].forEach((d, di) => {
          const sup = Math.abs(nrm[0]) * w / 2 + Math.abs(nrm[1]) * h / 2;
          const cx = P.x + nrm[0] * (d + sup), cy = P.y + nrm[1] * (d + sup);
          const box = { x: Math.round(cx - w / 2), y: Math.round(cy - h / 2), w, h };
          let score = 0;
          placed.forEach((o) => { if (overlap(box, o)) score += 20; });
          fixed.forEach((o) => { if (overlap(box, o)) score += 20; });
          segs.forEach((sg) => { if (segHitsRect(sg.p, sg.q, { x: box.x - 2, y: box.y - 2, w: box.w + 4, h: box.h + 4 })) score += sg.edge === i ? 8 : 4; });
          score += fi * 0.6 + di * 0.8; // prefer the middle of the edge and a short distance
          if (!best || score < best.score) best = { score, box };
        });
      });
    });
    placed.push(best.box);
    boxes[i] = best.box;
  });

  geo.forEach((gx, i) => {
    const e = gx.e;
    s += `<transition><source ref="${t.name}_${e.from}"/><target ref="${t.name}_${e.to}"/>`;
    const box = boxes[i];
    let ky = box ? box.y : 0;
    const put = (kind, v) => { if (v) { s += `<label kind="${kind}" x="${box.x}" y="${ky}">${esc(v)}</label>`; ky += LH; } };
    if (box) { put("select", e.select); put("guard", e.guard); put("synchronisation", e.sync); put("assignment", e.assign); }
    s += gx.nails.map((nl) => `<nail x="${nl[0]}" y="${nl[1]}"/>`).join("") + `</transition>\n`;
  });
  return s + `</template>\n`;
}

const TICK = { name: "Tick", init: "On", locs: [{ n: "On" }], trans: [{ from: "On", to: "On" }] }; // keeps `not deadlock` meaningful: a finished run may idle
function model(file, decl, templates, system, queries) {
  // supervision and plm have no Tick: they never end (the order cycle and the heartbeat keep going), and with the leads-to queries of plm a Tick that takes no time would let an infinite run stand still.
  if (file !== "supervision" && file !== "plm") { templates = templates.concat([TICK]); system = system.replace(";", ", Tick;"); }
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
    locs: [{ n: "Open", at: [0, -110] }, { n: "OpenQ", inv: "q <= DEPSTART", at: [0, 190] }, { n: "Blocked", inv: "y <= BLOCKMAX", at: [-380, 40] }, { n: "Took", committed: true, at: [260, 190] }, { n: "Done", at: [260, -110] }],
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
    locs: [{ n: "Idle", inv: "r <= RECONCILE", at: [150, 0] }, { n: "Adopted", at: [-300, 0] }],
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

// 5. PLM-WH: the state machine of the Papyrus model (internal/plm/machine.go) together with
// batter preparation asking for ingredients (BAT-DOSE repeats a request that is not answered).
model("plm", `// seconds, as in internal/plm and internal/timing
const int ORDER = 22;      // ORDER_SECONDS: a new production order every cycle
const int WITHDRAW = 3;    // the ingredients are collected: Withdrawal confirmation
const int RESTOCK = 15;    // Logistics restocks: Restock signal
const int BREAKER = 15;    // the circuit breaker stays open this long (timing.BreakerOpen)
const int WTO = 2;         // a request to the warehouse that takes longer is a wms_timeout
const int RETRY = 20;      // BAT repeats an ingredient request that is not answered (timing.IngredientRetry)
const int MAXOUT = 2;      // orders sent that BAT has not asked ingredients for
const int DOWNMAX = 40;    // the warehouse does not answer for at most this long (estimated)
int orders = 0;            // orders sent that BAT has not asked ingredients for yet (the map of sent orders)
int made = 0;              // orders created in the current order cycle
bool withdrawn = false;    // a withdrawal has been booked
bool gotConfirm = false;   // BAT has been confirmed
bool due = true;           // the order cycle ran out (the first order is due at once)
bool lowStock = false;     // not enough stock: FC1
bool wmsDown = false;      // the warehouse does not answer: FC2
clock age, up;             // age: time since BAT's first request, up: time since the warehouse answers again
urgent broadcast chan req;
broadcast chan confirmed, insufficient, staged;
`, [
  { name: "Plm", decl: "clock t, w, h, b;", init: "Idle", spread: 110,
    locs: [
      { n: "Idle", at: [-530, -170] }, { n: "Back", committed: true, at: [-150, -170] }, { n: "Verifying", committed: true, at: [-530, 0] },
      { n: "TimingOut", inv: "t <= WTO", at: [110, 0] }, { n: "Waiting", inv: "w <= WITHDRAW", at: [-150, 0] },
      { n: "Holding", inv: "h <= RESTOCK", at: [-530, 180] }, { n: "HoldAns", committed: true, at: [-340, 300] },
      { n: "Suspended", inv: "b <= BREAKER", at: [110, 180] }, { n: "SusAns", committed: true, at: [110, 320] } ],
    trans: [
      { from: "Idle", to: "Back", guard: "due" },
      { from: "Back", to: "Idle", guard: "due && orders < MAXOUT", assign: "due = false, orders++, made++" },
      { from: "Back", to: "Idle", guard: "!(due && orders < MAXOUT)", assign: "due = false" },
      { from: "Idle", to: "Verifying", sync: "req?" },
      { from: "Verifying", to: "TimingOut", guard: "wmsDown", assign: "t = 0", via: [[-150, 100]] },
      { from: "Verifying", to: "Holding", guard: "!wmsDown && lowStock", sync: "insufficient!", assign: "h = 0" },
      { from: "Verifying", to: "Waiting", guard: "!wmsDown && !lowStock", assign: "w = 0" },
      { from: "TimingOut", to: "Suspended", guard: "t >= WTO", sync: "staged!", assign: "b = 0" },
      { from: "Waiting", to: "Back", guard: "w >= WITHDRAW", sync: "confirmed!", assign: "withdrawn = true" },
      { from: "Waiting", to: "Waiting", sync: "req?" },
      { from: "Holding", to: "Verifying", guard: "h >= RESTOCK", assign: "lowStock = false" },
      { from: "Holding", to: "HoldAns", sync: "req?" },
      { from: "HoldAns", to: "Holding", sync: "insufficient!" },
      { from: "Suspended", to: "Back", guard: "b >= BREAKER && !wmsDown" },
      { from: "Suspended", to: "Suspended", guard: "b >= BREAKER && wmsDown", assign: "b = 0" },
      { from: "Suspended", to: "SusAns", sync: "req?" },
      { from: "SusAns", to: "Suspended", sync: "staged!" } ] },
  { name: "Bat", decl: "clock c;", init: "Ready",
    locs: [{ n: "Ready" }, { n: "Waiting", inv: "c <= RETRY" }, { n: "Retry", committed: true }],
    trans: [
      { from: "Ready", to: "Waiting", guard: "orders > 0", sync: "req!", assign: "orders--, c = 0, age = 0" },
      { from: "Waiting", to: "Ready", sync: "confirmed?", assign: "gotConfirm = true" },
      { from: "Waiting", to: "Ready", sync: "staged?" },
      { from: "Waiting", to: "Waiting", sync: "insufficient?", assign: "c = 0" },
      { from: "Waiting", to: "Retry", guard: "c >= RETRY" },
      { from: "Retry", to: "Waiting", sync: "req!", assign: "c = 0" } ] },
  { name: "Clock", decl: "clock c;", init: "Run",
    locs: [{ n: "Run", inv: "c <= ORDER" }],
    trans: [{ from: "Run", to: "Run", guard: "c >= ORDER", assign: "due = true, made = 0, c = 0" }] },
  { name: "Stock", init: "Normal",
    locs: [{ n: "Normal" }, { n: "Short" }],
    trans: [{ from: "Normal", to: "Short", assign: "lowStock = true" }] },
  { name: "Wms", decl: "clock x;", init: "Answers",
    locs: [{ n: "Answers" }, { n: "Silent", inv: "x <= DOWNMAX" }, { n: "Back" }],
    trans: [
      { from: "Answers", to: "Silent", assign: "wmsDown = true, x = 0" },
      { from: "Silent", to: "Back", assign: "wmsDown = false, up = 0" } ] },
], "system Plm, Bat, Clock, Stock, Wms;", [
  ["no deadlock", "A[] not deadlock"],
  ["at most MAXOUT orders wait for BAT to ask their ingredients", "A[] orders <= MAXOUT"],
  ["no burst of orders: never more than one per order cycle", "A[] made <= 1"],
  ["BAT is confirmed only after a withdrawal has been booked", "A[] gotConfirm imply withdrawn"],
  ["BAT gets its final answer (confirmed or staged) within RESTOCK + WITHDRAW = 18 s", "A[] Bat.Waiting imply age <= 18"],
  ["a held order is resolved", "Plm.Holding --> Plm.Idle"],
  ["communication resumes after a suspension", "Plm.Suspended --> Plm.Idle"],
  ["BAT is always answered in the end", "Bat.Waiting --> Bat.Ready"],
  ["after the warehouse answers again, the machine resumes within BREAKER + WTO (the suspension may have begun just before)", "A[] (Plm.Suspended && !wmsDown) imply up <= BREAKER + WTO"],
  ["an order can be held", "E<> Plm.Holding"],
  ["communication can be suspended", "E<> Plm.Suspended"],
  ["BAT can be confirmed", "E<> gotConfirm"],
  ["BAT can be confirmed although the stock was short and the warehouse silent", "E<> (gotConfirm && Stock.Short && Wms.Back)"],
]);
