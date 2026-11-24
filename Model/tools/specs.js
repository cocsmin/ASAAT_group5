const { build } = require("./smgen.js");
process.chdir(__dirname + "/../PapyrusProject");
const which = process.argv[2];

if (which === "BT") build("BT", {
  title: "BT Section Control STM", name: "BT SM1 Section Control", region: "Section control", frame: [1000, 700],
  states: [
    { n: "Initial", initial: true, x: 40, y: 20 },
    { n: "Running", x: 80, y: 70, w: 330, h: 90, doa: "move pans at the target speed", entry: "set the section to the target speed", exit: "publish Band state change" },
    { n: "Slowed", x: 600, y: 70, w: 320, h: 90, doa: "move pans at reduced speed; a Section command may lead to any state", entry: "set the reduced section speed" },
    { n: "Section Fault", x: 80, y: 430, w: 330, h: 120, doa: "wait for Repair confirmed", entry: "halt every section before it, publish error event and ticket bt-section-N (FC19)", exit: "run the section again" },
    { n: "Correcting", x: 600, y: 450, w: 320, h: 80, doa: "request the speed correction", entry: "note the wrong speed (FC20)" },
    { n: "Halted", x: 600, y: 250, w: 330, h: 120, doa: "finish the pans inside, take no new pans", entry: "halt this section and every section before it", exit: "restart the sections before it one by one, 1.5 s apart" },
  ],
  trans: [
    { from: "Initial", to: "Running" },
    { from: "Running", to: "Slowed", trig: "Section command", guard: "slow", sa: "(1.0,0.25)", ta: "(0.0,0.25)" },
    { from: "Slowed", to: "Running", trig: "Section command", guard: "run", sa: "(0.0,0.75)", ta: "(1.0,0.75)" },
    { from: "Running", to: "Section Fault", trig: "drive stops (FC19)", sa: "(0.15,1.0)", ta: "(0.15,0.0)" },
    { from: "Section Fault", to: "Running", trig: "Repair confirmed", lx: -55, sa: "(0.85,0.0)", ta: "(0.85,1.0)" },
    { from: "Running", to: "Correcting", trig: "speed differs from target", sa: "(0.6,1.0)", ta: "(0.0,0.2)" },
    { from: "Correcting", to: "Running", trig: "speed back in range", sa: "(0.0,0.6)", ta: "(0.85,1.0)" },
    { from: "Correcting", to: "Section Fault", trig: "correction fails", sa: "(0.0,0.9)", ta: "(1.0,0.8)" },
    { from: "Running", to: "Halted", trig: "Section command", guard: "halt", sa: "(1.0,0.7)", ta: "(0.0,0.2)" },
    { from: "Halted", to: "Running", trig: "Section command", guard: "run, owner is back", sa: "(0.0,0.7)", ta: "(1.0,0.97)" },
  ],
});

if (which === "MON") {
  build("MON", {
    title: "MON Node Watchdog STM", name: "MON SM1 Node Watchdog", region: "Node watchdog", frame: [1000, 700],
    states: [
      { n: "Initial", initial: true, x: 20, y: 60 },
      { n: "Unknown", x: 80, y: 40, w: 300, h: 70, doa: "wait for a birth certificate" },
      { n: "Online", x: 520, y: 40, w: 340, h: 90, doa: "watch the heartbeat of the node", entry: "release the band sections halted for this node" },
      { n: "Silent", x: 560, y: 300, w: 330, h: 90, doa: "keep the sections halted", entry: "mark the node faulty (FC28), halt its band sections" },
      { n: "Dead", x: 80, y: 300, w: 330, h: 90, doa: "wait for the container to restart", entry: "halt the band sections of the node" },
      { n: "Escalated", x: 80, y: 520, w: 330, h: 90, doa: "wait for the container to restart", entry: "error event FC27, Maintenance ticket for the container" },
    ],
    trans: [
      { from: "Initial", to: "Unknown" },
      { from: "Unknown", to: "Online", trig: "birth certificate" },
      { from: "Online", to: "Silent", trig: "no heartbeat for 3 x T_hb", sa: "(0.8,1.0)", ta: "(0.8,0.0)" },
      { from: "Silent", to: "Online", trig: "heartbeat", sa: "(0.2,0.0)", ta: "(0.2,1.0)" },
      { from: "Online", to: "Dead", trig: "death certificate", sa: "(0.1,1.0)", ta: "(0.9,0.0)" },
      { from: "Silent", to: "Dead", trig: "death certificate", sa: "(0.0,0.7)", ta: "(1.0,0.7)" },
      { from: "Dead", to: "Online", trig: "birth certificate", sa: "(1.0,0.1)", ta: "(0.3,1.0)" },
      { from: "Dead", to: "Escalated", trig: "no restart after 45 s", sa: "(0.5,1.0)", ta: "(0.5,0.0)" },
      { from: "Escalated", to: "Online", trig: "birth certificate", effect: "close the container ticket", sa: "(1.0,0.5)", ta: "(0.5,1.0)", pts: [[410, 565], [690, 565], [690, 130]] },
    ],
  });
  build("MON", {
    title: "MON Ticket Lifecycle STM", name: "MON SM2 Ticket Lifecycle", region: "Ticket lifecycle", frame: [760, 640],
    states: [
      { n: "Initial", initial: true, x: 300, y: 10 },
      { n: "Open", x: 120, y: 80, w: 420, h: 100, doa: "show the ticket on the dashboard", entry: "open a ticket for the machine, one open ticket per machine" },
      { n: "Closed", x: 120, y: 330, w: 420, h: 80, entry: "close the ticket" },
      { n: "Final", final: true, x: 320, y: 520 },
    ],
    trans: [
      { from: "Initial", to: "Open", trig: "error event with a machine id" },
      { from: "Open", to: "Closed", trig: "Repair confirmed", lx: -50, sa: "(0.12,1.0)", ta: "(0.12,0.0)" },
      { from: "Open", to: "Closed", trig: "container reborn", guard: "container ticket", lx: 50, sa: "(0.88,1.0)", ta: "(0.88,0.0)" },
      { from: "Closed", to: "Final" },
    ],
  });
}
