// Re-layout of the Cake Factory IBD and its 13 connectors (generated, checked on screen).
const fs = require("fs"), crypto = require("crypto");
process.chdir(__dirname + "/../PapyrusProject");
const A = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_";
const id = () => "_" + Array.from(crypto.randomBytes(22)).map((b) => A[b % 64]).join("");
const rd = (f) => { const s = fs.readFileSync(f, "utf8"); return [s.replace(/\r\n/g, "\n"), s.includes("\r\n")]; };
const wr = (f, s, crlf) => fs.writeFileSync(f, crlf ? s.split("\n").join("\r\n") : s);
const W = 440;

// ---------- 1. model: two top-level ports that were missing, so the signals can be drawn
let [FIN, cF] = rd("FIN.uml"), [BT, cB] = rd("BT.uml");
const PALLET = id(), BAND = id();
{
  const a = '<packagedElement xmi:type="uml:Class" xmi:id="_JB5r8Lz8EfGC__yJIGIlPQ"';
  const i = FIN.indexOf(a); if (i < 0) throw new Error("FIN class");
  const j = FIN.indexOf("\n", i) + 1;
  FIN = FIN.slice(0, j) + `      <ownedAttribute xmi:type="uml:Port" xmi:id="${PALLET}" name="Pallet ready for shipment" aggregation="composite"/>\n` + FIN.slice(j);
  const b = '<packagedElement xmi:type="uml:Class" xmi:id="_rh_68MSnEfG8fKg8yLcw1w"';
  const k = BT.indexOf(b); if (k < 0) throw new Error("BT class");
  const l = BT.indexOf("\n", k) + 1;
  BT = BT.slice(0, l) + `      <ownedAttribute xmi:type="uml:Port" xmi:id="${BAND}" name="Band state change" aggregation="composite"/>\n` + BT.slice(l);
}
wr("FIN.uml", FIN, cF); wr("BT.uml", BT, cB);

// ---------- 2. layout table: part -> {x,y,h, ports: name -> [side, slot]}
const PARTS = {
  plm: { prop: "_lNvSssA1EfGhFcn1z0k66Q", file: "PLM", x: 40, y: 60, h: 440, ports: {
    "Production order and recipe targets": ["R", 140], "Ingredient request and reservation result": ["R", 180], "Pallet ready for shipment": ["T", 60] } },
  bat: { prop: "_dNAWcMA1EfGhFcn1z0k66Q", file: "BAT", x: 600, y: 60, h: 440, ports: {
    "Production order and recipe targets": ["L", 140], "Ingredient request and reservation result": ["L", 180],
    "Batter ready": ["R", 60], "Depositing stopped / working again": ["R", 100] } },
  thm: { prop: "_IndJkMBHEfGiqthpeiudKA", file: "THM", x: 1160, y: 60, h: 500, ports: {
    "Batter ready": ["L", 60], "Depositing stopped / working again": ["L", 100],
    "Pan request": ["R", 140], "Pan for washing": ["R", 180], "Clean pan supply": ["R", 310], "Stop / resume pan admission": ["R", 350], "Cooled cake": ["R", 390],
    "Band state change": ["B", 210] } },
  fin: { prop: "_sJLa0MA1EfGhFcn1z0k66Q", file: "FIN", x: 1720, y: 60, h: 500, ports: {
    "Pan request": ["L", 140], "Pan for washing": ["L", 180], "Clean pan supply": ["L", 310], "Stop / resume pan admission": ["L", 350], "Cooled cake": ["L", 390],
    "Band state change": ["B", 210], "Pallet ready for shipment": ["T", 60] } },
  bt: { prop: "_o4pZENMLEfGVktM5bqmNpQ", file: "BT", x: 1440, y: 730, h: 160, ports: { "Band state change": ["T", 210] } },
  mon: { prop: "_vLblwNMLEfGVktM5bqmNpQ", file: "MON", x: 840, y: 730, h: 160, ports: { "sensorData": ["T", 210] } },
};
const pos = (P, side, slot) => side === "L" ? [-10, slot] : side === "R" ? [W - 10, slot] : side === "T" ? [slot, -10] : [slot, P.h - 10];

// ---------- 3. notation
let [N, cN] = rd("ASAAT.notation");
const names = {};
for (const f of ["BAT", "PLM", "FIN", "THM", "MON", "BT"]) {
  const [u] = rd(f + ".uml");
  for (const m of u.matchAll(/xmi:id="([^"]+)"[^>]*?name="([^"]*)"/g)) names[f + ".uml#" + m[1]] = m[2].replace(/&amp;/g, "&");
}
const d0 = N.indexOf('name="Cake Factory IBD"'), d1 = N.indexOf("</notation:Diagram>", d0);
let D = N.slice(d0, d1);
const shapeIds = {}; // part -> {portName -> shapeId}
// part shapes
for (const [key, P] of Object.entries(PARTS)) {
  const i = D.indexOf(`<element xmi:type="uml:Property" href="ASAAT.uml#${P.prop}"/>`);
  if (i < 0) throw new Error("no part " + key);
  const start = D.lastIndexOf('type="Property_Shape"', i);
  let block = D.slice(start, i);
  // ports inside the block
  shapeIds[key] = {};
  const found = new Set();
  block = block.replace(/(<children xmi:type="notation:Shape" xmi:id="([^"]+)" type="Port_Shape">)([\s\S]*?)(<element xmi:type="uml:Port" href="([^"]+)"\/>\s*<layoutConstraint xmi:type="notation:Bounds" xmi:id="[^"]+") x="-?\d+" y="-?\d+"(\/>)(\s*<\/children>)/g,
    (m, a, sid, mid, el, href, c, close) => {
      const nm = names[href]; const spec = P.ports[nm];
      if (!spec) return ""; // the port stays in the model, it is not drawn in this diagram
      shapeIds[key][nm] = sid; found.add(nm);
      const [x, y] = pos(P, spec[0], spec[1]);
      const len = (nm.length + 21) * 6.3;
      const lab = spec[0] === "L" ? [22, -11] : spec[0] === "R" ? [-Math.round(len + 24), -11] : spec[0] === "T" ? [20, 18] : [20, -30];
      mid = mid.replace(/(type="Port_NameLabel">\s*<layoutConstraint xmi:type="notation:Location" xmi:id="[^"]+") x="-?\d+" y="-?\d+"/, `$1 x="${lab[0]}" y="${lab[1]}"`);
      return a + mid + el + ` x="${x}" y="${y}"` + c + close;
    });
  // ports of the layout table that have no shape yet: add (FIN pallet, BT band, PLM none)
  const missing = Object.keys(P.ports).filter((n) => !found.has(n));
  let add = "";
  for (const nm of missing) {
    const href = nm === "Pallet ready for shipment" && key === "fin" ? "FIN.uml#" + PALLET : nm === "Band state change" && key === "bt" ? "BT.uml#" + BAND : null;
    if (!href) throw new Error("cannot add " + key + " / " + nm);
    const sid = id(); shapeIds[key][nm] = sid;
    const [x, y] = pos(P, P.ports[nm][0], P.ports[nm][1]);
    add += `          <children xmi:type="notation:Shape" xmi:id="${sid}" type="Port_Shape">
            <children xmi:type="notation:DecorationNode" xmi:id="${id()}" type="Port_NameLabel">
              <layoutConstraint xmi:type="notation:Location" xmi:id="${id()}" x="20" y="${P.ports[nm][0] === "T" ? 18 : -30}"/>
            </children>
            <children xmi:type="notation:DecorationNode" xmi:id="${id()}" type="Port_StereotypeLabel">
              <layoutConstraint xmi:type="notation:Location" xmi:id="${id()}" x="25" y="-10"/>
            </children>
            <element xmi:type="uml:Port" href="${href}"/>
            <layoutConstraint xmi:type="notation:Bounds" xmi:id="${id()}" x="${x}" y="${y}"/>
          </children>\n`;
  }
  // part bounds (the last Bounds with width/height after the ports)
  let tail = D.slice(i);
  tail = tail.replace(/(<layoutConstraint xmi:type="notation:Bounds" xmi:id="[^"]+") x="-?\d+" y="-?\d+" width="\d+" height="\d+"/, `$1 x="${P.x}" y="${P.y}" width="${W}" height="${P.h}"`);
  D = D.slice(0, start) + block + add + tail;
}
// the frame
D = D.replace(/(<element xmi:type="uml:Class" href="ASAAT.uml#_4ivN8MA0EfGhFcn1z0k66Q"\/>\s*<layoutConstraint xmi:type="notation:Bounds" xmi:id="[^"]+") (?:x="-?\d+" )?(?:y="-?\d+" )?width="\d+" height="\d+"/, `$1 x="40" width="2320" height="1030"`);

// ---------- 4. connectors
const CONN = [
  ["plm", "Production order and recipe targets", "bat", "Production order and recipe targets"],
  ["plm", "Ingredient request and reservation result", "bat", "Ingredient request and reservation result"],
  ["bat", "Batter ready", "thm", "Batter ready"],
  ["bat", "Depositing stopped / working again", "thm", "Depositing stopped / working again"],
  ["thm", "Pan request", "fin", "Pan request"],
  ["thm", "Pan for washing", "fin", "Pan for washing"],
  ["thm", "Clean pan supply", "fin", "Clean pan supply"],
  ["thm", "Stop / resume pan admission", "fin", "Stop / resume pan admission"],
  ["thm", "Cooled cake", "fin", "Cooled cake"],
  ["fin", "Pallet ready for shipment", "plm", "Pallet ready for shipment", { route: "pallet" }],
  ["bt", "Band state change", "thm", "Band state change", { route: "band-thm" }],
  ["bt", "Band state change", "fin", "Band state change", { route: "band-fin" }],
  ["bt", "Band state change", "mon", "sensorData", { route: "band-mon" }],
];
const portIdOf = {}; // key|name -> href
for (const [key, P] of Object.entries(PARTS)) {
  for (const nm of Object.keys(P.ports)) {
    const m = [...Object.entries(names)].find(([h, n]) => h.startsWith(P.file + ".uml#") && n === nm && true);
    portIdOf[key + "|" + nm] = null;
  }
}
// resolve port hrefs from the notation (exact ones used by the shapes)
const hrefOfShape = {};
for (const m of D.matchAll(/<children xmi:type="notation:Shape" xmi:id="([^"]+)" type="Port_Shape">[\s\S]*?<element xmi:type="uml:Port" href="([^"]+)"\/>/g)) hrefOfShape[m[1]] = m[2];

let U, cU; [U, cU] = rd("ASAAT.uml");
let connXml = "", stereo = "", edges = "";
const [FX, FY] = [0, 0];
function abs(key, nm) { const P = PARTS[key]; const [x, y] = pos(P, P.ports[nm][0], P.ports[nm][1]); return [P.x + x + 10, P.y + y + 10]; }
for (const c of CONN) {
  const [k1, n1, k2, n2, opt] = c;
  const cid = id(), e1 = id(), e2 = id();
  const h1 = hrefOfShape[shapeIds[k1][n1]], h2 = hrefOfShape[shapeIds[k2][n2]];
  connXml += `      <ownedConnector xmi:type="uml:Connector" xmi:id="${cid}">
        <end xmi:type="uml:ConnectorEnd" xmi:id="${e1}" partWithPort="${PARTS[k1].prop}">
          <role xmi:type="uml:Port" href="${h1}"/>
        </end>
        <end xmi:type="uml:ConnectorEnd" xmi:id="${e2}" partWithPort="${PARTS[k2].prop}">
          <role xmi:type="uml:Port" href="${h2}"/>
        </end>
      </ownedConnector>\n`;
  stereo += `  <Blocks:NestedConnectorEnd xmi:id="${id()}" base_Element="${e1}" propertyPath="${PARTS[k1].prop}" base_ConnectorEnd="${e1}"/>\n`;
  stereo += `  <Blocks:NestedConnectorEnd xmi:id="${id()}" base_Element="${e2}" propertyPath="${PARTS[k2].prop}" base_ConnectorEnd="${e2}"/>\n`;
  let pts = [[0, 0], [0, 0]];
  if (opt && opt.route) {
    const a = abs(k1, n1), b = abs(k2, n2);
    const OFFX = +(process.env.OFFX || 0), OFFY = +(process.env.OFFY || 0);
    const L = (p) => [p[0] + OFFX, p[1] + OFFY];
    if (opt.route === "pallet") pts = [a, [a[0], 20], [b[0], 20], b].map(L);
    else { const my = 650; pts = [a, [a[0], my], [b[0], my], b].map(L); if (opt.route === "band-mon") pts = [a, [a[0], my], [b[0], my], b].map(L); }
  }
  const ptxt = pts.map((p) => `[${p[0]}, ${p[1]}, -643984, -643984]`).join("$");
  edges += `    <edges xmi:type="notation:Connector" xmi:id="${id()}" type="Connector_Edge" source="${shapeIds[k1][n1]}" target="${shapeIds[k2][n2]}">
      <children xmi:type="notation:DecorationNode" xmi:id="${id()}" type="Connector_StereotypeLabel">
        <layoutConstraint xmi:type="notation:Location" xmi:id="${id()}" x="1" y="58"/>
      </children>
      <children xmi:type="notation:DecorationNode" xmi:id="${id()}" type="Connector_NameLabel">
        <layoutConstraint xmi:type="notation:Location" xmi:id="${id()}" x="-1" y="-19"/>
      </children>
      <children xmi:type="notation:DecorationNode" xmi:id="${id()}" type="Connector_SourceMultiplicityLabel">
        <layoutConstraint xmi:type="notation:Location" xmi:id="${id()}" x="-13" y="18"/>
      </children>
      <children xmi:type="notation:DecorationNode" xmi:id="${id()}" type="Connector_TargetMultiplicityLabel">
        <layoutConstraint xmi:type="notation:Location" xmi:id="${id()}" x="13" y="19"/>
      </children>
      <styles xmi:type="notation:FontStyle" xmi:id="${id()}"/>
      <element xmi:type="uml:Connector" href="ASAAT.uml#${cid}"/>
      <bendpoints xmi:type="notation:RelativeBendpoints" xmi:id="${id()}" points="${ptxt}"/>
      <sourceAnchor xmi:type="notation:IdentityAnchor" xmi:id="${id()}" id="(0.5,0.5)"/>
      <targetAnchor xmi:type="notation:IdentityAnchor" xmi:id="${id()}" id="(0.5,0.5)"/>
    </edges>\n`;
}
// UML: connectors into the Cake Factory class (before its closing tag), stereotypes at the end
{
  const i = U.indexOf('<packagedElement xmi:type="uml:Class" xmi:id="_4ivN8MA0EfGhFcn1z0k66Q"');
  const j = U.indexOf("    </packagedElement>", i);
  if (i < 0 || j < 0) throw new Error("cake factory class");
  U = U.slice(0, j) + connXml + U.slice(j);
  const t = U.lastIndexOf("</xmi:XMI>");
  U = U.slice(0, t) + stereo + U.slice(t);
}
// notation: drop the old edges of this diagram? (it has the stereotype-comment link only) -> append ours before diagram end
D = D + edges;
N = N.slice(0, d0) + D + N.slice(d1);
wr("ASAAT.uml", U, cU); wr("ASAAT.notation", N, cN);
console.log("IBD done");
