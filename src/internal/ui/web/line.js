// Live picture of the production line. It draws the stations in the order of the
// line, a conveyor with items that move while a section runs and stop when it
// halts, the band sections underneath, the pan loop, and a callout where
// something is wrong. It is redrawn from every snapshot of the monitor.
(function () {
  var NS = 'http://www.w3.org/2000/svg';

  // Stations: id, label, owner node, devices that decide the health, the number
  // shown on the badge, and a small line of text.
  var ST = [
    { id: 'wh',   t: 'Warehouse',   n: 'PLM', x: 30,   y: 110, w: 100, d: ['WarehouseInventory', 'OrderScheduler', 'RecipeManager'], b: function (g) { return g['orders.held'] || null; }, s: function (g) { return g.breaker ? 'breaker open' : (g['orders.created'] || 0) + ' orders'; } },
    { id: 'dos',  t: 'Dosing',      n: 'BAT', x: 160,  y: 110, w: 100, d: ['Doser', 'LoadCell'], b: function (g) { return g['stage.dosing'] || null; } },
    { id: 'mix',  t: 'Mixing',      n: 'BAT', x: 290,  y: 110, w: 100, d: ['Mixer', 'ConsistencySensor'], b: function (g) { return g['stage.mixing'] || null; } },
    { id: 'ins',  t: 'Inspection',  n: 'BAT', x: 420,  y: 110, w: 100, d: ['InspectionStation'], b: function (g) { return g['stage.inspection'] || null; }, s: function (g) { return g['stage.waiting'] ? 'batter waits' : 'human check'; } },
    { id: 'dep',  t: 'Depositing',  n: 'THM', x: 570,  y: 110, w: 100, d: ['Depositor', 'PanPresenceEye', 'CheckWeigher'], b: function (g) { return g.queue || null; }, s: function (g) { return (g.deposited || 0) + ' pans filled'; } },
    { id: 'bake', t: 'Baking tunnel', n: 'THM', x: 700, y: 110, w: 240, d: ['Zone1', 'Zone2', 'Zone3'], b: function (g) { return g.tunnel || null; }, zones: true },
    { id: 'exit', t: 'Exit check',  n: 'THM', x: 960,  y: 110, w: 90,  d: ['HeatCamera'], b: function (g) { return null; }, s: function (g) { return '97 °C ± 0.5'; } },
    { id: 'cool', t: 'Cooling',     n: 'THM', x: 1070, y: 110, w: 100, d: ['CoolingControl'], b: function (g) { return g.cooling || null; } },
    { id: 'buf',  t: 'Cooling buffer', n: 'THM', x: 945, y: 205, w: 105, h: 44, d: ['CoolingBuffer'], b: function (g) { return g.buffer || null; } },
    { id: 'dpn',  t: 'Depanning',   n: 'FIN', x: 1070, y: 300, w: 100, d: ['Depanner', 'VisionPicker'], b: function (g) { return g.depanQ || null; } },
    { id: 'qua',  t: 'Quality check', n: 'FIN', x: 950, y: 300, w: 100, d: ['Checkweigher', 'VisionCamera', 'MetalDetector'], s: function (g) { return (g.rejected || 0) + ' rejected'; } },
    { id: 'wrp',  t: 'Wrap + carton', n: 'FIN', x: 830, y: 300, w: 100, d: ['Wrapper', 'Cartoner', 'CaseCheckweigher', 'HoldingArea'], b: function (g) { return g.holding || null; } },
    { id: 'pal',  t: 'Pallet',      n: 'FIN', x: 710,  y: 300, w: 100, d: ['PalletPacker'], b: function (g) { return g.palletFill || null; }, s: function (g) { return (g.pallets || 0) + ' pallets'; } },
    { id: 'shp',  t: 'Shipment',    n: 'PLM', x: 590,  y: 300, w: 100, d: ['TransportPlanner'], s: function (g) { return (g['cakes.shipped'] || 0) + ' cakes out'; } },
    { id: 'wash', t: 'Pan washing', n: 'FIN', x: 1070, y: 430, w: 100, d: ['WashingStation'], b: function (g) { return g.washQ || null; } },
    { id: 'bt', t: 'Band Transport', n: 'BT', x: 170, y: 330, w: 250, d: ['ConveyorController', 'ConveyorDrive', 'SpeedSensor', 'SectionStatusSensor'], b: function (g) { var c = 0; for (var i = 1; i <= 5; i++) { if ((g['state' + i] || 0) > 0) { c++; } } return c || null; }, s: function (g) { return 'target ' + (g.target === undefined ? '–' : g.target) + ' cm/s · bars S1–S5'; } },
    { id: 'pool', t: 'Clean pans',  n: 'FIN', x: 830,  y: 430, w: 100, d: ['PanPool'], b: function (g) { return g.pool; }, s: function () { return 'ready'; }, pool: true }
  ];
  var H = 64;
  function byId(id) { for (var i = 0; i < ST.length; i++) { if (ST[i].id === id) { return ST[i]; } } }

  // Conveyor segments: path, kind of item, the band section it belongs to.
  var SEG = [
    { id: 'c1',  k: 'batter', p: 'M130 142 H160', a: 'wh',  z: 'dos' },
    { id: 'c2',  k: 'batter', p: 'M260 142 H290', a: 'dos', z: 'mix' },
    { id: 'c3',  k: 'batter', p: 'M390 142 H420', a: 'mix', z: 'ins' },
    { id: 'c4',  k: 'batter', p: 'M520 142 H570', a: 'ins', z: 'dep' },
    { id: 'c5',  k: 'cake',   p: 'M670 142 H700', a: 'dep', z: 'bake', s: 1 },
    { id: 'c6',  k: 'cake',   p: 'M940 142 H960', a: 'bake', z: 'exit', s: 2 },
    { id: 'c7',  k: 'cake',   p: 'M1050 142 H1070', a: 'exit', z: 'cool', s: 3 },
    { id: 'c8',  k: 'cake',   p: 'M1140 174 V300', a: 'cool', z: 'dpn', s: 3 },
    { id: 'c9',  k: 'cake',   p: 'M1070 332 H1050', a: 'dpn', z: 'qua', s: 4 },
    { id: 'c10', k: 'cake',   p: 'M950 332 H930', a: 'qua', z: 'wrp', s: 4 },
    { id: 'c11', k: 'cake',   p: 'M830 332 H810', a: 'wrp', z: 'pal', s: 5 },
    { id: 'c12', k: 'cake',   p: 'M710 332 H690', a: 'pal', z: 'shp', s: 5 },
    { id: 'c13', k: 'pan',    p: 'M1100 364 V430', a: 'dpn', z: 'wash', s: 4 },
    { id: 'c14', k: 'pan',    p: 'M1070 462 H930', a: 'wash', z: 'pool' },
    { id: 'c15', k: 'pan',    p: 'M830 462 H530 V232 H620 V174', a: 'pool', z: 'dep' },
    { id: 'c16', k: 'buf',    p: 'M1085 174 Q1070 205 1050 227', a: 'cool', z: 'buf', s: 3 }
  ];
  var BANDS = [
    { s: 1, x: 570, y: 180, w: 100, l: 'S1' }, { s: 2, x: 700, y: 180, w: 240, l: 'S2' }, { s: 3, x: 960, y: 180, w: 210, l: 'S3' },
    { s: 4, x: 950, y: 372, w: 220, l: 'S4' }, { s: 5, x: 710, y: 372, w: 220, l: 'S5' }
  ];
  var GROUP = { PLM: 'Production & Logistics', BAT: 'Batter', THM: 'Thermal', FIN: 'Finishing & Packaging' };

  var built = false, svg, prev = {};
  function el(name, attrs, parent) {
    var e = document.createElementNS(NS, name);
    for (var k in attrs) { e.setAttribute(k, attrs[k]); }
    if (parent) { parent.appendChild(e); }
    return e;
  }
  function trunc(s, n) { s = String(s || ''); return s.length > n ? s.slice(0, n - 1) + '…' : s; }

  function build() {
    svg = document.getElementById('lineviz');
    if (!svg) { return false; }
    // legend of the groups, the line titles
    el('text', { x: 30, y: 62, 'class': 'vz-title' }, svg).textContent = 'Batter';
    el('text', { x: 570, y: 62, 'class': 'vz-title' }, svg).textContent = 'Thermal processing';
    el('text', { x: 1070, y: 280, 'class': 'vz-title' }, svg).textContent = '';
    el('text', { x: 590, y: 282, 'class': 'vz-title' }, svg).textContent = 'Finishing & packaging';
    el('text', { x: 560, y: 520, 'class': 'vz-note' }, svg).textContent = 'pan loop: washed pans return to depositing';
    var rails = el('g', { id: 'vz-rails' }, svg);
    SEG.forEach(function (sg) {
      var g = el('g', { 'class': 'vz-seg ' + sg.k, id: 'vz-' + sg.id }, rails);
      el('path', { d: sg.p, 'class': 'vz-rail' }, g);
      var probe = el('path', { d: sg.p }, svg);
      var len = probe.getTotalLength();
      svg.removeChild(probe);
      var dur = Math.max(1.6, len / 55);
      var n = Math.max(1, Math.min(9, Math.round(len / 48)));
      for (var i = 0; i < n && sg.k === 'batter'; i++) { // pans and cakes are drawn from the real documents
        var d = el('circle', { r: sg.k === 'pan' ? 5 : 4.5, 'class': 'vz-dot' }, g);
        d.style.offsetPath = "path('" + sg.p + "')";
        d.style.setProperty('--d', dur + 's');
        d.style.animationDelay = (-(dur * i) / n) + 's';
      }
    });
    BANDS.forEach(function (b) {
      var g = el('g', { 'class': 'vz-band', id: 'vz-band' + b.s }, svg);
      el('rect', { x: b.x, y: b.y, width: b.w, height: 7, rx: 3 }, g);
      el('text', { x: b.x + b.w - 4, y: b.y + 17, 'class': 'vz-bandl' }, g).textContent = b.l;
    });
    ST.forEach(function (s) {
      var h = s.h || H;
      var g = el('g', { 'class': 'vz-st', id: 'vz-st-' + s.id }, svg);
      el('title', {}, g).textContent = s.t;
      el('rect', { x: s.x, y: s.y, width: s.w, height: h, rx: 10, 'class': 'vz-box' }, g);
      el('rect', { x: s.x, y: s.y, width: s.w, height: 6, rx: 3, 'class': 'vz-stripe g-' + s.n }, g);
      el('text', { x: s.x + s.w / 2, y: s.y + 25, 'class': 'vz-name', 'text-anchor': 'middle' }, g).textContent = s.t;
      el('text', { x: s.x + s.w / 2, y: s.y + h - 12, 'class': 'vz-sub', 'text-anchor': 'middle', id: 'vz-sub-' + s.id }, g);
      var badge = el('g', { 'class': 'vz-badge', id: 'vz-badge-' + s.id }, g);
      el('circle', { cx: s.x + s.w - 4, cy: s.y + 4, r: 11 }, badge);
      el('text', { x: s.x + s.w - 4, y: s.y + 8, 'text-anchor': 'middle' }, badge);
      if (s.zones) {
        for (var z = 0; z < 3; z++) {
          var zx = s.x + 14 + z * 72;
          var zg = el('g', { 'class': 'vz-zone', id: 'vz-zone' + (z + 1) }, g);
          el('rect', { x: zx, y: s.y + 33, width: 64, height: 22, rx: 5 }, zg);
          el('text', { x: zx + 32, y: s.y + 48, 'text-anchor': 'middle' }, zg);
        }
      }
      var bang = el('g', { 'class': 'vz-bang', id: 'vz-bang-' + s.id }, g);
      el('circle', { cx: s.x + 4, cy: s.y + 4, r: 11 }, bang);
      el('text', { x: s.x + 4, y: s.y + 9, 'text-anchor': 'middle' }, bang).textContent = '!';
    });
    built = true;
    return true;
  }

  function stState(n, devs) {
    if (!n) { return { k: 'off', d: 'no data' }; }
    if (n.status === 'dead') { return { k: 'down', d: 'container down' }; }
    if (n.status === 'silent') { return { k: 'down', d: 'not responding' }; }
    if (n.status === 'unknown') { return { k: 'off', d: 'waiting' }; }
    var w = window.worst ? window.worst(n, devs) : { r: 0, d: '' };
    var k = w.r >= 4 ? 'bad' : w.r === 3 ? 'off' : w.r === 2 ? 'warn' : w.r === 1 ? 'idle' : 'ok';
    return { k: k, d: w.detail || '' };
  }

  function flash(id, text, cls) {
    var s = byId(id);
    var t = el('text', { x: s.x + s.w / 2, y: s.y - 4, 'class': 'vz-flash ' + cls, 'text-anchor': 'middle' }, svg);
    t.textContent = text;
    setTimeout(function () { if (t.parentNode) { t.parentNode.removeChild(t); } }, 1700);
  }

  function draw(snap) {
    if (!built && !build()) { return; }
    var nodes = {};
    snap.nodes.forEach(function (n) { nodes[n.id] = n; });
    var secs = {};
    snap.sections.forEach(function (s) { secs[s.id] = s.state; });
    var state = {};

    ST.forEach(function (s) {
      var n = nodes[s.n];
      var g = n ? n.gauges : {};
      var st = stState(n, s.d);
      state[s.id] = st.k;
      var box = document.getElementById('vz-st-' + s.id);
      var busy = s.b && s.b(g) > 0 && (st.k === 'ok' || st.k === 'idle');
      box.setAttribute('class', 'vz-st st-' + st.k + (busy ? ' busy' : ''));
      var sub = document.getElementById('vz-sub-' + s.id);
      var bad = st.k === 'bad' || st.k === 'warn' || st.k === 'down';
      sub.textContent = bad ? trunc(st.d, 24) : (s.s ? s.s(g) : '');
      box.firstChild.textContent = s.t + (st.d ? ' – ' + st.d : '');
      var badge = document.getElementById('vz-badge-' + s.id);
      var cnt = s.b ? s.b(g) : null;
      if (cnt !== null && cnt !== undefined && (cnt > 0 || s.pool)) {
        badge.style.display = '';
        badge.lastChild.textContent = cnt;
      } else { badge.style.display = 'none'; }
      document.getElementById('vz-bang-' + s.id).style.display = (st.k === 'bad' || st.k === 'down') ? '' : 'none';
      if (s.zones) {
        for (var z = 1; z <= 3; z++) {
          var zg = document.getElementById('vz-zone' + z);
          var dev = n && n.devices ? n.devices['Zone' + z] : null;
          var zk = !dev ? 'ok' : dev.state === 'FAULT' ? 'bad' : dev.state === 'DEGRADED' ? 'warn' : 'ok';
          zg.setAttribute('class', 'vz-zone z-' + zk);
          zg.lastChild.textContent = (g['zone' + z] !== undefined ? g['zone' + z] : '–') + ' °C';
        }
      }
    });

    SEG.forEach(function (sg) {
      var k = 'run';
      var a = state[sg.a], z = state[sg.z];
      if (a === 'down' || z === 'down' || a === 'off' || z === 'off') { k = 'off'; }
      else if (sg.s) {
        var ss = secs[sg.s];
        k = ss === 'run' ? 'run' : ss === 'slow' ? 'slow' : ss === 'off' ? 'off' : 'halt';
      }
      if (k === 'run' && (a === 'bad' || z === 'bad')) { k = 'halt'; }
      document.getElementById('vz-' + sg.id).setAttribute('class', 'vz-seg ' + sg.k + ' ' + k);
    });
    BANDS.forEach(function (b) {
      document.getElementById('vz-band' + b.s).setAttribute('class', 'vz-band b-' + (secs[b.s] || 'run'));
    });

    // short animations when something is discarded or shipped
    var g = function (id, name) { return nodes[id] && nodes[id].gauges ? (nodes[id].gauges[name] || 0) : 0; };
    var checks = [
      ['THM.discarded', g('THM', 'discarded'), 'exit', '✕ discard', 'bad'],
      ['FIN.rejected', g('FIN', 'rejected'), 'qua', '✕ reject', 'bad'],
      ['BAT.batches.discarded', g('BAT', 'batches.discarded'), 'dos', '✕ batch lost', 'bad'],
      ['PLM.pallets', g('PLM', 'pallets.shipped'), 'shp', '+1 pallet', 'good']
    ];
    checks.forEach(function (c) {
      if (prev[c[0]] !== undefined && c[1] > prev[c[0]]) { flash(c[2], c[3], c[4]); }
      prev[c[0]] = c[1];
    });

    // node chips above the picture
    var chips = document.getElementById('nodechips');
    if (chips) {
      chips.innerHTML = snap.nodes.map(function (n) {
        var cls = n.status === 'online' ? 'ok' : n.status === 'unknown' ? '' : 'bad';
        var txt = n.status === 'online' ? 'online' : n.status === 'dead' ? 'container down' : n.status === 'silent' ? 'not responding' : 'waiting';
        return '<span class="pill ' + cls + '"><i></i><span>' + n.id + ' ' + txt + (n.restarts ? ' · ' + n.restarts + ' restart' + (n.restarts > 1 ? 's' : '') : '') + '</span></span>';
      }).join('');
    }
  }

  // ---- the real pans: one marker per pan document in MongoDB, placed by its station
  var panLayer = null, panEls = {}, panSeen = {}, pathLen = {};
  function pathOf(id) { for (var i = 0; i < SEG.length; i++) { if (SEG[i].id === id) { return SEG[i].p; } } }
  function pointOn(id, f) {
    var p = document.querySelector('#vz-' + id + ' .vz-rail');
    if (!p) { return { x: 0, y: 0 }; }
    var len = p.getTotalLength();
    return p.getPointAtLength(len * Math.max(0, Math.min(1, f)));
  }
  var TRAVEL = 7000; // ms a pan needs to ride a conveyor segment
  // Where a pan is: a station (box id) or a conveyor segment with progress.
  function whereIs(p) {
    var age = Date.now() - new Date(p.updated).getTime();
    switch (p.station) {
      case 'FIN-PanPool': return { box: 'pool' };
      case 'FIN-PanSupply': return age < TRAVEL * 1.6 ? { seg: 'c15', f: age / (TRAVEL * 1.6) } : { box: 'dep' };
      case 'THM-Depositing': return { box: 'dep' };
      case 'THM-Baking': return { box: 'bake' };
      case 'THM-TunnelExit': return { box: 'exit' };
      case 'THM-CoolingBuffer': return { box: 'buf' };
      case 'THM-Cooling': return { box: 'cool' };
      case 'FIN-Depanning':
        // the empty pan rides to the washing station (emptied or depanned: both are queued for washing)
        return age < TRAVEL ? { seg: 'c13', f: age / TRAVEL } : { box: 'wash' };
      case 'FIN-Washing': return { box: 'wash' };
    }
    return null;
  }
  var PCOL = { clean: '#9aa4b2', supplied: '#9aa4b2', filled: '#f3d98a', baking: '#e0832f', baked: '#c9793a', buffered: '#6aa5d8', cooling: '#4f8fd0', cooled: '#4f8fd0', depanned: '#b0b7c3', emptied: '#b0b7c3' };
  // Cakes after depanning have their own document.
  function cakeAt(p) {
    var age = Date.now() - new Date(p.updated).getTime();
    switch (p.status) {
      case 'awaiting inspection': return { box: 'qua' };
      case 'inspected': case 'wrapped': return { box: 'wrp' };
      case 'packed': return age < 30000 ? { box: 'pal' } : null;
    }
    return null;
  }
  function drawPans(pans) {
    if (!built) { return; }
    if (!panLayer) { panLayer = el('g', { id: 'vz-pans' }, svg); }
    var perBox = {}, seen = {};
    pans.filter(function (p) { return /^(PAN|CAKE)-/.test(p.id); }).sort(function (a, b) { return a.id < b.id ? -1 : 1; }).forEach(function (p) {
      var isCake = /^CAKE-/.test(p.id);
      var w = isCake ? cakeAt(p) : whereIs(p);
      if (!w) { return; }
      var x, y;
      if (w.seg) { var pt = pointOn(w.seg, w.f); x = pt.x; y = pt.y; }
      else {
        var st = byId(w.box), i = perBox[w.box] = (perBox[w.box] || 0) + 1, j = i - 1 + (isCake ? 0 : 0);
        var cols = Math.max(1, Math.floor((st.w - 12) / 17));
        x = st.x + 12 + (j % cols) * 17;
        y = st.y + (w.box === 'bake' ? 59 : 38) + Math.floor(j / cols) * 14; // the tunnel box keeps its zone temperatures above the pans
        if (w.box !== 'bake' && y > st.y + (st.h || 64) - 16) { y = st.y + (st.h || 64) - 16; }
      }
      seen[p.id] = true;
      var g = panEls[p.id];
      if (!g) {
        g = panEls[p.id] = el('g', { 'class': 'vz-pan' + (isCake ? ' cake' : '') }, panLayer);
        el('title', {}, g);
        if (isCake) { el('circle', { r: 6 }, g); } else { el('rect', { x: -7, y: -5, width: 14, height: 10, rx: 3 }, g); }
        el('text', { x: 0, y: 3, 'text-anchor': 'middle' }, g);
        g.style.transform = 'translate(' + x + 'px,' + y + 'px)';
      }
      g.style.transform = 'translate(' + x + 'px,' + y + 'px)';
      g.firstChild.textContent = p.id + ': ' + p.status + ' at ' + p.station + (p.fillWeight ? ' · ' + p.fillWeight.toFixed(0) + ' g' : '');
      g.children[1].setAttribute('fill', isCake ? '#c9793a' : (PCOL[p.status] || '#9aa4b2'));
      g.lastChild.textContent = isCake ? '' : p.id.replace(/^PAN-0*/, '');
    });
    Object.keys(panEls).forEach(function (id) {
      if (!seen[id]) { var g = panEls[id]; if (g.parentNode) { g.parentNode.removeChild(g); } delete panEls[id]; }
    });
  }
  var lastPans = [];
  function pollPans() {
    fetch('/api/pans?limit=400').then(function (r) { return r.json(); }).then(function (p) { lastPans = p; drawPans(p); }).catch(function () {});
  }
  setInterval(pollPans, 1000);
  setInterval(function () { drawPans(lastPans); }, 250);

  window.drawLine = draw;
})();
