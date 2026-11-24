// Optimization with UPPAAL: for one parameter of a model, find by bisection the largest (or smallest)
// value for which a set of queries is still satisfied, and report the margin to the value the
// implementation uses. Run from src/docs/uppaal after `node gen.js`:
//
//   node optimize.js            # runs every experiment and writes optimization.md
//
// `sup` queries do not terminate on these models (clocks that are never reset), so the exact numbers
// come from bisection over A[] queries: UPPAAL is the oracle, each step is one verifyta run.
// Needs verifyta (UPPAAL 5): set VERIFYTA if it is not at the default path.
const fs = require("fs");
const os = require("os");
const path = require("path");
const { spawnSync } = require("child_process");

const VERIFYTA = process.env.VERIFYTA || "C:/Program Files/UPPAAL-5.0.0/app/bin/verifyta.exe";
const TMP = fs.mkdtempSync(path.join(os.tmpdir(), "uppaal-opt-"));

function holds(model, consts, queries) {
  let xml = fs.readFileSync(model + ".xml", "utf8");
  for (const [name, v] of Object.entries(consts)) {
    const re = new RegExp("(const int " + name + "\\s*=\\s*)\\d+");
    if (!re.test(xml)) throw new Error("no const " + name + " in " + model);
    xml = xml.replace(re, "$1" + v);
  }
  const x = path.join(TMP, "m.xml"), q = path.join(TMP, "m.q");
  fs.writeFileSync(x, xml);
  fs.writeFileSync(q, queries.join("\n") + "\n");
  const r = spawnSync(VERIFYTA, ["-q", x, q], { encoding: "utf8", timeout: 120000 });
  if (r.error || r.status === null) throw new Error("verifyta did not finish for " + model + " " + JSON.stringify(consts));
  const out = r.stdout + r.stderr;
  const sat = (out.match(/Formula is satisfied/g) || []).length;
  const not = (out.match(/Formula is NOT satisfied/g) || []).length;
  if (sat + not !== queries.length) throw new Error("unexpected verifyta output: " + out.slice(0, 300));
  return not === 0;
}

// largest v in [lo, hi] with the queries satisfied (they are satisfied for small v), or smallest when dir = "min"
function bisect(model, name, lo, hi, queries, dir, fixed = {}) {
  let good = null;
  if (dir === "max") {
    if (!holds(model, { ...fixed, [name]: lo }, queries)) return null;
    while (lo < hi) {
      const mid = Math.ceil((lo + hi) / 2);
      if (holds(model, { ...fixed, [name]: mid }, queries)) lo = mid; else hi = mid - 1;
    }
    return lo;
  }
  if (!holds(model, { ...fixed, [name]: hi }, queries)) return null;
  while (lo < hi) {
    const mid = Math.floor((lo + hi) / 2);
    if (holds(model, { ...fixed, [name]: mid }, queries)) hi = mid; else lo = mid + 1;
  }
  return lo;
}

const BATSIDE = "A[] (Batch.Mixing || Batch.Inspecting || Batch.Holding) imply g <= LOST";
const TOOK = "A[] Dep.Took imply g <= LOST";

const experiments = [
  { title: "Supervision: how slow may a Docker restart be?", model: "supervision", param: "RDMAX", dir: "max", lo: 1, hi: 120, used: 30,
    queries: ["A[] (Env.Faulted && !Node.Online) imply t <= RESTORE", "A[] not MON.Escalated"],
    meaning: "Largest restart time for which a node is back within 60 s (THM-REQ-73) and MON never escalates to Maintenance (45 s). The code assumes at most 30 s." },
  { title: "Supervision: how late may the broker declare a node dead?", model: "supervision", param: "DEADAFTER", dir: "max", lo: 1, hi: 60, used: 8,
    queries: ["A[] (Node.Down && MON.Watching) imply t <= DETECT"],
    meaning: "Largest detection time that still meets the 15 s of THM-REQ-72. The broker gives 7.5 s (1.5 x keep-alive of 5 s), so the keep-alive could be up to this / 1.5." },
  { title: "Batter: smallest safe batch-lost limit of dosing", model: "batter", param: "LOST", dir: "min", lo: 1, hi: 300, used: 120,
    queries: [BATSIDE, TOOK],
    meaning: "Smallest limit after which dosing may give up a batch without ever dropping one that is still being made or deposited. The code uses 120 s." },
  { title: "Batter: largest wait T_batter", model: "batter", param: "T_batter", dir: "max", lo: 1, hi: 150, used: 25,
    queries: [BATSIDE, TOOK],
    meaning: "Largest batter wait for which the LOST limit of 120 s still holds, with one UC4 rejection. The group fixed 25 s from food safety, not from this bound." },
  { title: "Batter: longest stop of depositing", model: "batter", param: "BLOCKMAX", dir: "max", lo: 1, hi: 300, used: 60,
    queries: ["A[] Dep.Expired imply g <= LOST"],
    meaning: "Longest stop of depositing (estimated at 60 s) for which even a discard at depositing happens before dosing gives the batch up. A negative margin is not a failure: at the estimated 60 s the late discard (126 s after the batch started) comes after dosing gave up, so it only drops a batch dosing already replaced; nothing is deposited from it (the Took query holds for any stop length)." },
  { title: "PLM: largest restock time", model: "plm", param: "RESTOCK", dir: "max", lo: 1, hi: 60, used: 15,
    queries: ["A[] Bat.Waiting imply age <= RETRY"],
    meaning: "Largest Logistics restock time for which BAT still gets its final answer before it repeats the request after 20 s (with the withdrawal of 3 s)." },
  { title: "PLM: smallest BAT retry time", model: "plm", param: "RETRY", dir: "min", lo: 1, hi: 60, used: 20,
    queries: ["A[] Bat.Waiting imply age <= RETRY"],
    meaning: "Smallest retry time of BAT that is longer than the final answer (restock 15 s + withdrawal 3 s). The code uses 20 s." },
  { title: "Handover: largest reconcile period", model: "handover", param: "RECONCILE", dir: "max", lo: 1, hi: 60, used: 5,
    queries: ["A[] (stored && !adopted) imply a <= 8"],
    meaning: "Largest scan period for which a stored document is taken up within 8 s (TIM-06) with a grace of 3 s." },
];

const rows = [];
for (const e of experiments) {
  process.stderr.write(e.title + " ... ");
  let v;
  try { v = bisect(e.model, e.param, e.lo, e.hi, e.queries, e.dir); } catch (err) { v = "error: " + err.message; }
  process.stderr.write(String(v) + "\n");
  rows.push({ ...e, v });
}

let md = "# Optimization with UPPAAL\n\n" +
  "Generated by `node optimize.js` (src/docs/uppaal). For each parameter, UPPAAL (`verifyta`) is asked by bisection\n" +
  "for the largest or smallest value for which the listed queries stay satisfied. This shows how much room the\n" +
  "design values have: the **margin** is the distance between the value used and the value UPPAAL still accepts.\n" +
  "All other parameters keep the values of the models (estimated ones are marked in the model files).\n\n" +
  "| Experiment | Parameter | Used | UPPAAL accepts | Margin | Queries |\n|---|---|---|---|---|---|\n";
for (const r of rows) {
  const lim = typeof r.v === "number" ? (r.dir === "max" ? "up to " + r.v : "from " + r.v) : String(r.v);
  const margin = typeof r.v === "number" ? (r.dir === "max" ? r.v - r.used : r.used - r.v) : "";
  md += `| ${r.title} | \`${r.param}\` (${r.model}) | ${r.used} | ${lim} | ${margin} | ${r.queries.map((q) => "`" + q + "`").join("<br>")} |\n`;
}
md += "\n## What each line means\n\n" + rows.map((r) => `- **${r.title}** ${r.meaning}`).join("\n") + "\n";
fs.writeFileSync("optimization.md", md);
console.log(md);
