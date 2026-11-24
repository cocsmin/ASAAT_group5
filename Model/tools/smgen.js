// Generates UML state machines and their Papyrus diagrams (BT.uml/.notation, MON.uml/.notation)
// in the same structure Papyrus itself writes (copied from the BAT diagrams).
const fs = require("fs");
const crypto = require("crypto");
const A = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_";
const id = () => "_" + Array.from(crypto.randomBytes(22)).map((b) => A[b % 64]).join("");
const esc = (s) => String(s).replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;");

// spec: { file: "BT", title: "Band Transport", name: "SM1 Section Control", states: [...], trans: [...], frame: [w,h] }
// state: { n, x, y, w, h, entry, doa, exit }; special: { n:"Initial", initial:true, x,y } { n:"Final", final:true, x,y }
function build(file, spec) {
  const uml = fs.readFileSync(file + ".uml", "utf8");
  const nota = fs.readFileSync(file + ".notation", "utf8");
  const crlfU = uml.includes("\r\n"), crlfN = nota.includes("\r\n");
  let U = uml.replace(/\r\n/g, "\n"), N = nota.replace(/\r\n/g, "\n");

  const smId = id(), regId = id();
  const S = {}; // state name -> {id, ...}
  for (const st of spec.states) { st.id = id(); st.shapeId = id(); S[st.n] = st; for (const k of ["entry", "doa", "exit"]) if (st[k]) st[k + "Id"] = id(); }

  // ---- events (signals) for triggers
  const sigIds = {}, evIds = {};
  let evXml = "";
  for (const t of spec.trans) {
    if (!t.trig) continue;
    if (!evIds[t.trig]) {
      sigIds[t.trig] = id(); evIds[t.trig] = id();
      evXml += `    <packagedElement xmi:type="uml:Signal" xmi:id="${sigIds[t.trig]}" name="${esc(t.trig)}"/>\n`;
      evXml += `    <packagedElement xmi:type="uml:SignalEvent" xmi:id="${evIds[t.trig]}" name="${esc(t.trig)}" signal="${sigIds[t.trig]}"/>\n`;
    }
  }
  for (const t of spec.trans) t.id = id();

  // ---- UML
  let x = `    <packagedElement xmi:type="uml:StateMachine" xmi:id="${smId}" name="${esc(spec.name)}">\n`;
  x += `      <region xmi:type="uml:Region" xmi:id="${regId}" name="${esc(spec.region || spec.name)}">\n`;
  for (const t of spec.trans) {
    const g = t.guard ? id() : null;
    x += `        <transition xmi:type="uml:Transition" xmi:id="${t.id}"${t.label ? ` name="${esc(t.label)}"` : ""}${g ? ` guard="${g}"` : ""} source="${S[t.from].id}" target="${S[t.to].id}">\n`;
    if (g) x += `          <ownedRule xmi:type="uml:Constraint" xmi:id="${g}" name="${esc(t.guard)}">\n            <specification xmi:type="uml:LiteralString" xmi:id="${id()}" value="${esc(t.guard)}"/>\n          </ownedRule>\n`;
    if (t.effect) { t.effId = id(); x += `          <effect xmi:type="uml:OpaqueBehavior" xmi:id="${t.effId}" name="${esc(t.effect)}"/>\n`; }
    if (t.trig) x += `          <trigger xmi:type="uml:Trigger" xmi:id="${id()}" name="${esc(t.trig)}" event="${evIds[t.trig]}"/>\n`;
    x += `        </transition>\n`;
  }
  for (const st of spec.states) {
    if (st.initial) x += `        <subvertex xmi:type="uml:Pseudostate" xmi:id="${st.id}" name="${esc(st.n)}"/>\n`;
    else if (st.final) x += `        <subvertex xmi:type="uml:FinalState" xmi:id="${st.id}" name="${esc(st.n)}"/>\n`;
    else {
      x += `        <subvertex xmi:type="uml:State" xmi:id="${st.id}" name="${esc(st.n)}">\n`;
      if (st.doa) x += `          <doActivity xmi:type="uml:OpaqueBehavior" xmi:id="${st.doaId}" name="${esc(st.doa)}"/>\n`;
      if (st.entry) x += `          <entry xmi:type="uml:OpaqueBehavior" xmi:id="${st.entryId}" name="${esc(st.entry)}"/>\n`;
      if (st.exit) x += `          <exit xmi:type="uml:OpaqueBehavior" xmi:id="${st.exitId}" name="${esc(st.exit)}"/>\n`;
      x += `        </subvertex>\n`;
    }
  }
  x += `      </region>\n    </packagedElement>\n`;
  const at = U.lastIndexOf("  </uml:Model>");
  if (at < 0) throw new Error("no </uml:Model> in " + file);
  U = U.slice(0, at) + evXml + x + U.slice(at);

  // ---- notation
  const href = (i) => `${file}.uml#${i}`;
  const [FW, FH] = spec.frame;
  const dId = id();
  const css = (k) => `<eAnnotations xmi:type="ecore:EAnnotation" xmi:id="${id()}" source="PapyrusCSSForceValue">\n<details xmi:type="ecore:EStringToStringMapEntry" xmi:id="${id()}" key="${k}" value="true"/>\n</eAnnotations>`;
  let shapes = "";
  for (const st of spec.states) {
    if (st.initial) {
      shapes += `            <children xmi:type="notation:Shape" xmi:id="${st.shapeId}" type="Pseudostate_InitialShape">
              <children xmi:type="notation:DecorationNode" xmi:id="${id()}" type="Pseudostate_InitialFloatingNameLabel">
                <layoutConstraint xmi:type="notation:Location" xmi:id="${id()}" x="25" y="3"/>
              </children>
              <children xmi:type="notation:DecorationNode" xmi:id="${id()}" type="Pseudostate_InitialStereotypeLabel">
                <layoutConstraint xmi:type="notation:Location" xmi:id="${id()}" x="25" y="-10"/>
              </children>
              <element xmi:type="uml:Pseudostate" href="${href(st.id)}"/>
              <layoutConstraint xmi:type="notation:Bounds" xmi:id="${id()}" x="${st.x}" y="${st.y}"/>
            </children>\n`;
    } else if (st.final) {
      shapes += `            <children xmi:type="notation:Shape" xmi:id="${st.shapeId}" type="FinalState_Shape">
              <children xmi:type="notation:DecorationNode" xmi:id="${id()}" type="FinalState_FloatingNameLabel">
                <layoutConstraint xmi:type="notation:Location" xmi:id="${id()}" x="25" y="3"/>
              </children>
              <children xmi:type="notation:DecorationNode" xmi:id="${id()}" type="FinalState_StereotypeLabel">
                <layoutConstraint xmi:type="notation:Location" xmi:id="${id()}" x="25" y="-10"/>
              </children>
              <element xmi:type="uml:FinalState" href="${href(st.id)}"/>
              <layoutConstraint xmi:type="notation:Bounds" xmi:id="${id()}" x="${st.x}" y="${st.y}" width="20" height="20"/>
            </children>\n`;
    } else {
      const nameH = st.h - 16;
      let beh = "";
      for (const [k, type, tag] of [["doa", "Behavior_DoActivityBehaviorLabel", "doaId"], ["entry", "Behavior_EntryBehaviorLabel", "entryId"], ["exit", "Behavior_ExitBehaviorLabel", "exitId"]]) {
        if (!st[k]) continue;
        beh += `              <children xmi:type="notation:Shape" xmi:id="${id()}" type="${type}">
                ${css("mutable")}
                <element xmi:type="uml:OpaqueBehavior" href="${href(st[tag])}"/>
                <layoutConstraint xmi:type="notation:Location" xmi:id="${id()}" x="-10" y="-10"/>
              </children>\n`;
      }
      shapes += `            <children xmi:type="notation:Shape" xmi:id="${st.shapeId}" type="State_Shape">
              ${css("mutable")}
              <children xmi:type="notation:DecorationNode" xmi:id="${id()}" type="State_NameLabel">
                <layoutConstraint xmi:type="notation:Bounds" xmi:id="${id()}" width="${st.w}" height="${nameH}"/>
              </children>
              <children xmi:type="notation:DecorationNode" xmi:id="${id()}" type="State_FloatingNameLabel">
                <layoutConstraint xmi:type="notation:Location" xmi:id="${id()}" x="40"/>
              </children>
              <children xmi:type="notation:BasicCompartment" xmi:id="${id()}" type="State_RegionCompartment">
                <layoutConstraint xmi:type="notation:Bounds" xmi:id="${id()}" y="${nameH}" width="${st.w}" height="16"/>
              </children>
              <children xmi:type="notation:BasicCompartment" xmi:id="${id()}" type="compartment_shape_display">
                <styles xmi:type="notation:TitleStyle" xmi:id="${id()}"/>
                <layoutConstraint xmi:type="notation:Bounds" xmi:id="${id()}"/>
              </children>
${beh}              <element xmi:type="uml:State" href="${href(st.id)}"/>
              <layoutConstraint xmi:type="notation:Bounds" xmi:id="${id()}" x="${st.x}" y="${st.y}" width="${st.w}" height="${st.h}"/>
            </children>\n`;
    }
  }
  let edges = "";
  for (const t of spec.trans) {
    const sa = t.sa ? `\n      <sourceAnchor xmi:type="notation:IdentityAnchor" xmi:id="${id()}" id="${t.sa}"/>` : "";
    const ta = t.ta ? `\n      <targetAnchor xmi:type="notation:IdentityAnchor" xmi:id="${id()}" id="${t.ta}"/>` : "";
    const pts = (t.pts || [[0, 0], [0, 0]]).map((p) => `[${p[0]}, ${p[1]}, -643984, -643984]`).join("$");
    edges += `    <edges xmi:type="notation:Connector" xmi:id="${id()}" type="Transition_Edge" source="${S[t.from].shapeId}" target="${S[t.to].shapeId}">
      <children xmi:type="notation:DecorationNode" xmi:id="${id()}" type="Transition_NameLabel">
        <layoutConstraint xmi:type="notation:Location" xmi:id="${id()}"${t.lx ? ` x="${t.lx}"` : ""} y="${20 + (t.ly || 0)}"/>
      </children>
      <children xmi:type="notation:DecorationNode" xmi:id="${id()}" type="Transition_GuardLabel">
        <layoutConstraint xmi:type="notation:Location" xmi:id="${id()}"${t.lx ? ` x="${t.lx}"` : ""} y="${-20 + (t.ly || 0)}"/>
      </children>
      <children xmi:type="notation:DecorationNode" xmi:id="${id()}" type="Transition_StereotypeLabel">
        <layoutConstraint xmi:type="notation:Location" xmi:id="${id()}"${t.lx ? ` x="${t.lx}"` : ""} y="${60 + (t.ly || 0)}"/>
      </children>
      <styles xmi:type="notation:FontStyle" xmi:id="${id()}"/>
      <element xmi:type="uml:Transition" href="${href(t.id)}"/>
      <bendpoints xmi:type="notation:RelativeBendpoints" xmi:id="${id()}" points="${pts}"/>${sa}${ta}
    </edges>\n`;
  }
  const sId = id(), rcId = id();
  const diagram = `  <notation:Diagram xmi:id="${dId}" type="PapyrusUMLStateMachineDiagram" name="${esc(spec.title)}" measurementUnit="Pixel">
    <children xmi:type="notation:Shape" xmi:id="${sId}" type="StateMachine_Shape">
      <children xmi:type="notation:DecorationNode" xmi:id="${id()}" type="StateMachine_NameLabel">
        <layoutConstraint xmi:type="notation:Bounds" xmi:id="${id()}" width="${FW}" height="21"/>
      </children>
      <children xmi:type="notation:BasicCompartment" xmi:id="${rcId}" type="StateMachine_RegionCompartment">
        <children xmi:type="notation:Shape" xmi:id="${id()}" type="Region_Shape">
          <eAnnotations xmi:type="ecore:EAnnotation" xmi:id="${id()}" source="RegionAnnotationKey">
            <details xmi:type="ecore:EStringToStringMapEntry" xmi:id="${id()}" key="RegionZoneKey" value=""/>
          </eAnnotations>
          <children xmi:type="notation:BasicCompartment" xmi:id="${id()}" type="Region_SubvertexCompartment">
${shapes}            <layoutConstraint xmi:type="notation:Bounds" xmi:id="${id()}"/>
          </children>
          <element xmi:type="uml:Region" href="${href(regId)}"/>
          <layoutConstraint xmi:type="notation:Bounds" xmi:id="${id()}" y="60" width="${FW}" height="${FH}"/>
        </children>
        <layoutConstraint xmi:type="notation:Bounds" xmi:id="${id()}" y="21" width="${FW}" height="${FH - 5}"/>
      </children>
      <element xmi:type="uml:StateMachine" href="${href(smId)}"/>
      <layoutConstraint xmi:type="notation:Bounds" xmi:id="${id()}" x="30" y="30" width="${FW}" height="${FH + 16}"/>
    </children>
    <styles xmi:type="notation:StringValueStyle" xmi:id="${id()}" name="diagram_compatibility_version" stringValue="1.4.0"/>
    <styles xmi:type="notation:DiagramStyle" xmi:id="${id()}"/>
    <styles xmi:type="style:PapyrusDiagramStyle" xmi:id="${id()}" diagramKindId="org.eclipse.papyrus.sysml16.diagram.stateMachine">
      <owner xmi:type="uml:StateMachine" href="${href(smId)}"/>
    </styles>
    <element xmi:type="uml:StateMachine" href="${href(smId)}"/>
${edges}  </notation:Diagram>
`;
  const nat = N.lastIndexOf("</xmi:XMI>");
  N = N.slice(0, nat) + diagram + N.slice(nat);
  const NL = String.fromCharCode(10), CRLF = String.fromCharCode(13, 10);
  if (crlfU) U = U.split(NL).join(CRLF);
  if (crlfN) N = N.split(NL).join(CRLF);
  fs.writeFileSync(file + ".uml", U);
  fs.writeFileSync(file + ".notation", N);
  console.log("added", spec.title, "to", file);
}
module.exports = { build };
