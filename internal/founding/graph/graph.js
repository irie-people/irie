// The Charters Graph page (DESIGN §4.17, docs/proposals/founding-documents.md,
// "The explorer"): the charters as a map to explore. The texts lie on a
// timeline, a row for each place they were made, with the links between
// them; under them, the rights and ideas they name. Pick a text, a part or an
// idea and the map lights what it bears on, while the card beside it gives
// the words. The page is one file, loads nothing, and keeps no state but the
// pick in its address and the view in sessionStorage.
//
// Its motion follows public work: the camera's flights are van Wijk and
// Nuij's smooth zoom and pan, "Smooth and efficient zooming and panning"
// (IEEE InfoVis 2003), with rho = sqrt(2) as d3-zoom's default; the tree of
// parts is WAI-ARIA's tree view pattern (the ARIA Authoring Practices
// Guide); the wheel and gesture handling reads the events as MDN and WebKit
// document them. No code is copied from any of them.
(() => {
  'use strict';

  // ---- The data and its indexes --------------------------------------

  const G = JSON.parse(document.getElementById('charters-data').textContent);
  const byId = new Map(G.nodes.map(n => [n.id, n]));
  const docOf = new Map(G.docs.map(d => [d.id, d]));
  const texts = G.nodes.filter(n => n.kind === 'text');
  const ideas = G.nodes.filter(n => n.kind === 'idea');
  const families = new Map(G.families.map(f => [f.id, f]));
  const kids = new Map();
  const tiesFrom = new Map();
  const tiesTo = new Map();
  const push = (m, k, v) => { const a = m.get(k); if (a) a.push(v); else m.set(k, [v]); };
  for (const t of G.ties) {
    push(tiesFrom, t.from, t);
    push(tiesTo, t.to, t);
    if (t.how === 'in') push(kids, t.to, byId.get(t.from));
  }
  const linksOf = new Map();
  G.links.forEach((l, i) => { push(linksOf, l.from, i); push(linksOf, l.to, i); });

  const frac = when => {
    const [y, m = 1, d = 1] = when.split('-').map(Number);
    return y + (m - 1) / 12 + (d - 1) / 365;
  };
  for (const t of texts) {
    const d = docOf.get(t.id);
    Object.assign(t, { title: d.title, when: d.when, dkind: d.kind, about: d.about, sha: d.sha512, year: +d.when.slice(0, 4), t: frac(d.when) });
    t.size = d.blocks.reduce((n, b) => n + (b.text ? b.text.length : 0) + (b.items ? b.items.join('').length : 0), 0);
  }
  // A part's text, its depth and its blocks' parts.
  const partAt = new Map(); // doc id -> the innermost part of each block, or the text
  for (const t of texts) partAt.set(t.id, new Array(docOf.get(t.id).blocks.length).fill(t.id));
  for (const n of G.nodes) {
    if (n.kind !== 'part') continue;
    n.text = byId.get(n.doc);
    n.depth = 1;
    for (let up = byId.get(n.up); up && up.kind === 'part'; up = byId.get(up.up)) n.depth++;
  }
  for (const n of G.nodes) {
    if (n.kind !== 'part') continue;
    const at = partAt.get(n.doc);
    for (let i = n.span[0]; i < n.span[1]; i++) {
      const was = byId.get(at[i]);
      if (!was || was.kind !== 'part' || was.depth < n.depth) at[i] = n.id;
    }
  }
  // Which ideas each part and text names, and where each idea is named.
  const textOf = n => (n.kind === 'text' ? n : n.kind === 'part' ? n.text : null);
  for (const i of ideas) { i.uses = []; i.texts = new Map(); }
  for (const t of texts) t.ideas = new Map();
  for (const tie of G.ties) {
    if (tie.how !== 'names') continue;
    const idea = byId.get(tie.to);
    const from = byId.get(tie.from);
    const text = textOf(from);
    idea.uses.push(tie);
    idea.texts.set(text.id, (idea.texts.get(text.id) || 0) + 1);
    text.ideas.set(idea.id, (text.ideas.get(idea.id) || 0) + 1);
  }
  for (const i of ideas) {
    i.first = Math.min(...[...i.texts.keys()].map(id => byId.get(id).year));
    i.last = Math.max(...[...i.texts.keys()].map(id => byId.get(id).year));
  }

  // ---- Words: the search's folding, as the core's ----------------------

  const LETTER = /[\p{L}\p{Nd}]/u;
  // spans splits a passage into its words as the core's fold does, each
  // with where it is, so a search on folded words can light the passage's
  // own: an old spelling is today's word, at the old word's place.
  function spans(text) {
    const out = [];
    let w = '', s = -1;
    const flush = end => {
      if (w) {
        const today = G.spellings[w];
        if (today) for (const t of today.split(' ')) out.push({ w: t, s, e: end });
        else out.push({ w, s, e: end });
      }
      w = ''; s = -1;
    };
    for (let i = 0; i < text.length;) {
      const cp = text.codePointAt(i);
      const ch = String.fromCodePoint(cp);
      const n = cp > 0xffff ? 2 : 1;
      if (ch === "'" || ch === '’') { /* an apostrophe parts no words */ }
      else if (ch === '&') { flush(i); out.push({ w: 'and', s: i, e: i + n }); }
      else if (LETTER.test(ch)) { if (s < 0) s = i; w += ch.toLowerCase(); }
      else flush(i);
      i += n;
    }
    flush(text.length);
    return out;
  }
  const fold = text => spans(text).map(x => x.w).join(' ');

  // ---- Layout: when across, where down, ideas under them ---------------

  const LANE_H = 220, AXIS = 70, IDEA_GAP = 120, IDEA_ROW = 60, IDEA_SPACE = 130;
  const laneIndex = new Map(G.lanes.map((l, i) => [l.id, i]));
  const byDate = texts.slice().sort((a, b) => a.t - b.t || laneIndex.get(a.lane) - laneIndex.get(b.lane));
  {
    let x = 0, prev = null;
    for (const t of byDate) {
      if (prev) x += 30 + 16 * Math.log2(1 + Math.max(0, t.t - prev.t));
      t.x = x;
      prev = t;
    }
    // In a lane, texts close in time take turns above and below its line.
    const last = new Map();
    for (const t of byDate) {
      const li = laneIndex.get(t.lane);
      const rows = last.get(li) || [-1e9, -1e9, -1e9];
      const off = [0, -56, 56];
      let row = rows.findIndex(x => t.x - x >= 70);
      if (row < 0) row = rows.indexOf(Math.min(...rows));
      rows[row] = t.x;
      last.set(li, rows);
      t.y = AXIS + li * LANE_H + LANE_H / 2 + off[row];
      t.r = 6.5 + 2.6 * Math.log10(t.size / 1500 + 1);
    }
  }
  const lanesBottom = AXIS + G.lanes.length * LANE_H;
  const minX = byDate[0].x, maxX = byDate[byDate.length - 1].x;
  // An idea sits under the middle of the texts that name it.
  {
    const median = xs => { xs.sort((a, b) => a - b); const m = xs.length >> 1; return xs.length % 2 ? xs[m] : (xs[m - 1] + xs[m]) / 2; };
    for (const i of ideas) i.mx = median([...i.texts.keys()].map(id => byId.get(id).x));
    const rows = [];
    for (const i of ideas.slice().sort((a, b) => a.mx - b.mx)) {
      let row = rows.findIndex(x => i.mx - x >= IDEA_SPACE);
      if (row < 0) { rows.push(-1e9); row = rows.length - 1; }
      rows[row] = i.mx;
      i.x = i.mx;
      i.y = lanesBottom + IDEA_GAP + row * IDEA_ROW;
      i.r = 4 + 1.4 * Math.log2(1 + i.texts.size);
    }
    var ideaRows = rows.length;
  }
  const ideasTop = lanesBottom + IDEA_GAP - IDEA_ROW / 2 - 18;
  const world = { x0: minX - 260, x1: maxX + 160, y0: 0, y1: lanesBottom + IDEA_GAP + ideaRows * IDEA_ROW + 10 };
  // The year at an x, and the x of a year, between the texts' places.
  const xOfYear = y => {
    if (y <= byDate[0].t) return minX - (byDate[0].t - y) * 0.4;
    for (let i = 1; i < byDate.length; i++) {
      const a = byDate[i - 1], b = byDate[i];
      if (y <= b.t) return b.t === a.t ? a.x : a.x + (b.x - a.x) * (y - a.t) / (b.t - a.t);
    }
    return maxX + (y - byDate[byDate.length - 1].t) * 4;
  };

  // ---- Looks: the palette, read from the page's own colours ------------

  const canvas = document.getElementById('map');
  const ctx = canvas.getContext('2d');
  let C = {};
  const FONT = getComputedStyle(document.body).fontFamily;
  const hex = h => { h = h.trim().replace('#', ''); if (h.length === 3) h = [...h].map(c => c + c).join(''); return [0, 2, 4].map(i => parseInt(h.slice(i, i + 2), 16)); };
  const rgba = (c, a = 1) => `rgba(${c[0]},${c[1]},${c[2]},${a})`;
  const mix = (a, b, t) => a.map((v, i) => Math.round(v + (b[i] - v) * t));
  function readColours() {
    const css = getComputedStyle(document.documentElement);
    for (const k of ['sidebar', 'ground', 'card', 'panel', 'edge', 'ink', 'muted', 'accent', 'accent-fill', 'accent-ink', 'paused', 'frozen', 'sun', 'waves', 'done']) {
      C[k] = hex(css.getPropertyValue('--' + k));
    }
    C.dark = (0.299 * C.sidebar[0] + 0.587 * C.sidebar[1] + 0.114 * C.sidebar[2]) < 128;
    C.band = mix(C.sidebar, C.ground, C.dark ? 0.45 : 0.55);
  }
  readColours();
  // How a link looks, by its kind: colour and dashes, so colour is never
  // the only cue.
  const HOW = {
    'drew on': { c: 'accent', dash: [], w: 1.5, say: 'drew on' },
    amends: { c: 'frozen', dash: [], w: 2.2, say: 'amends' },
    replaced: { c: 'paused', dash: [8, 4], w: 1.6, say: 'replaced' },
    beside: { c: 'frozen', dash: [1.5, 4], w: 1.8, say: 'stands beside' },
    follows: { c: 'muted', dash: [5, 5], w: 1.3, say: 'follows' },
  };
  const HOWS = ['drew on', 'amends', 'replaced', 'beside', 'follows'];
  function howSample(how) {
    const s = HOW[how];
    return `<svg viewBox="0 0 26 8" aria-hidden="true"><line x1="1" y1="4" x2="25" y2="4" stroke="var(--${s.c})" stroke-width="${s.w}" stroke-linecap="round" stroke-dasharray="${s.dash.join(' ')}"/></svg>`;
  }

  // ---- The camera ------------------------------------------------------

  let W = 0, H = 0, dpr = 1;
  const cam = { x: (world.x0 + world.x1) / 2, y: (world.y0 + world.y1) / 2, k: 0.3 };
  let fitK = 0.3;
  const kMax = 3.2;
  const kMin = () => fitK * 0.6;
  const sx = x => (x - cam.x) * cam.k + W / 2;
  const sy = y => (y - cam.y) * cam.k + H / 2;
  const wx = x => (x - W / 2) / cam.k + cam.x;
  const wy = y => (y - H / 2) / cam.k + cam.y;
  const reduced = () => window.matchMedia('(prefers-reduced-motion: reduce)').matches;
  let anim = 0; // the camera's one animation frame
  let touched = 0; // when the person last moved the camera

  function stop() { if (anim) cancelAnimationFrame(anim); anim = 0; }
  function clampK(k) { return Math.max(kMin(), Math.min(kMax, k)); }

  function fitView(box = world, pad = 28) {
    const bw = Math.max(1, box.x1 - box.x0), bh = Math.max(1, box.y1 - box.y0);
    const k = Math.min((W - pad * 2) / bw, (H - pad * 2) / bh);
    return { x: (box.x0 + box.x1) / 2, y: (box.y0 + box.y1) / 2, k };
  }

  // go flies the camera to a view along van Wijk and Nuij's path, which
  // zooms out, pans and zooms in so the eye keeps its place.
  function go(to, ms) {
    stop();
    to.k = clampK(to.k);
    if (reduced() || !W) { Object.assign(cam, to); save(); draw(); return; }
    const rho = Math.SQRT2;
    const w0 = W / cam.k, w1 = W / to.k;
    const ux0 = cam.x, uy0 = cam.y, dx = to.x - ux0, dy = to.y - uy0;
    const d2 = dx * dx + dy * dy;
    let at, S;
    if (d2 < 1e-9) {
      S = Math.log(w1 / w0) / rho;
      at = t => [ux0, uy0, w0 * Math.exp(rho * t * S)];
    } else {
      const d1 = Math.sqrt(d2);
      const b0 = (w1 * w1 - w0 * w0 + rho ** 4 * d2) / (2 * w0 * rho * rho * d1);
      const b1 = (w1 * w1 - w0 * w0 - rho ** 4 * d2) / (2 * w1 * rho * rho * d1);
      const r0 = Math.log(Math.sqrt(b0 * b0 + 1) - b0);
      const r1 = Math.log(Math.sqrt(b1 * b1 + 1) - b1);
      S = (r1 - r0) / rho;
      at = t => {
        const s = t * S, c = Math.cosh(r0);
        const u = w0 / (rho * rho * d1) * (c * Math.tanh(rho * s + r0) - Math.sinh(r0));
        return [ux0 + u * dx, uy0 + u * dy, w0 * c / Math.cosh(rho * s + r0)];
      };
    }
    const dur = ms || Math.max(260, Math.min(700, Math.abs(S) * 600));
    const start = performance.now();
    const ease = t => (t < 0.5 ? 4 * t * t * t : 1 - (-2 * t + 2) ** 3 / 2);
    const step = now => {
      const t = Math.min(1, (now - start) / dur);
      const [x, y, w] = at(ease(t));
      cam.x = x; cam.y = y; cam.k = W / w;
      draw();
      if (t < 1) anim = requestAnimationFrame(step);
      else { anim = 0; Object.assign(cam, to); save(); draw(); }
    };
    anim = requestAnimationFrame(step);
  }

  // zoomAt zooms about a point on the screen, which stays still.
  function zoomAt(px, py, k1, animate) {
    stop();
    k1 = clampK(k1);
    const ax = wx(px), ay = wy(py);
    const set = k => { cam.k = k; cam.x = ax - (px - W / 2) / k; cam.y = ay - (py - H / 2) / k; };
    touched = performance.now();
    if (!animate || reduced()) { set(k1); save(); draw(); return; }
    const k0 = cam.k, start = performance.now(), dur = 220;
    const step = now => {
      const t = Math.min(1, (now - start) / dur);
      const e = 1 - (1 - t) ** 3;
      set(k0 * (k1 / k0) ** e);
      draw();
      if (t < 1) anim = requestAnimationFrame(step); else { anim = 0; save(); }
    };
    anim = requestAnimationFrame(step);
  }

  function panBy(dx, dy) {
    cam.x -= dx / cam.k;
    cam.y -= dy / cam.k;
    touched = performance.now();
    draw();
    save();
  }

  // glide carries a flick on, slowing as a thing on a table would.
  function glide(vx, vy) {
    stop();
    if (reduced()) return;
    let last = performance.now();
    const step = now => {
      const dt = Math.min(48, now - last);
      last = now;
      cam.x -= vx * dt / cam.k;
      cam.y -= vy * dt / cam.k;
      const f = 0.9 ** (dt / 16.7);
      vx *= f; vy *= f;
      draw();
      if (Math.hypot(vx, vy) > 0.012) anim = requestAnimationFrame(step); else { anim = 0; save(); }
    };
    anim = requestAnimationFrame(step);
  }

  // ---- What is picked, and what it lights ------------------------------

  let current = null; // the pick's id
  let hover = null; // the node under the pointer, or a row's
  const EMPTY = { texts: new Set(), links: new Set(), ideas: new Set(), spokes: [], callouts: new Map(), badges: new Map(), main: null };
  let lit = EMPTY;
  let light = EMPTY; // what the hover lights

  // partsUnder is a part and every part inside it.
  function partsUnder(n) {
    const out = [n];
    for (let i = 0; i < out.length; i++) for (const k of kids.get(out[i].id) || []) out.push(k);
    return out;
  }
  // namesOf is how often each idea is named in a part, all the parts in it
  // counted.
  function namesOf(n) {
    if (n.kind === 'text') return n.ideas;
    const m = new Map();
    for (const p of partsUnder(n)) for (const t of tiesFrom.get(p.id) || []) if (t.how === 'names') m.set(t.to, (m.get(t.to) || 0) + 1);
    return m;
  }
  // quotesOf are the link quotations a part holds, or the ones held in
  // its text that name it.
  function quotesOf(n) {
    const out = [];
    for (const p of partsUnder(n)) {
      for (const t of tiesFrom.get(p.id) || []) if (t.how === 'quotes') out.push({ tie: t, part: p, other: byId.get(t.to), mine: true });
      for (const t of tiesTo.get(p.id) || []) if (t.how === 'quotes') out.push({ tie: t, part: p, other: byId.get(t.from), mine: false });
    }
    return out;
  }
  // sameAs are the parts in other texts with the very same words, as the
  // Bill of Rights' ten amendments are the Constitution's first ten.
  const sameAs = n => [...(tiesFrom.get(n.id) || []).filter(t => t.how === 'same').map(t => Object.assign(Object.create(byId.get(t.to)), { exact: !!t.exact })),
    ...(tiesTo.get(n.id) || []).filter(t => t.how === 'same').map(t => Object.assign(Object.create(byId.get(t.from)), { exact: !!t.exact }))];
  const short = name => {
    let s = name.replace(/\*\*/g, '');
    const m = s.match(/^(Article \d+[A-Za-z]*|CAP\. [IVXLC]+|Section \d+|Amendment [IVXLC]+|Chapter \d+|Liberty \d+|§ [\w()]+)/);
    if (m) return m[1];
    const r = s.match(/^([IVXLC]+)\.\s/);
    if (r) return r[1];
    s = s.replace(/\.$/, '');
    if (s.length > 34) s = s.split(/[.;–—:] /)[0];
    if (s.length > 34) s = s.slice(0, 32).replace(/\s+\S*$/, '') + '…';
    return s;
  };
  const label = n => (n.kind === 'part' ? short(n.name) : n.name);

  function lights(id) {
    const n = id && byId.get(id);
    if (!n) return EMPTY;
    const L = { texts: new Set(), links: new Set(), ideas: new Set(), spokes: [], callouts: new Map(), badges: new Map(), main: n };
    const callout = (text, part) => { const a = L.callouts.get(text.id) || []; if (!a.includes(part)) a.push(part); L.callouts.set(text.id, a); };
    if (n.kind === 'text') {
      L.texts.add(n.id);
      for (const i of linksOf.get(n.id) || []) { const l = G.links[i]; L.links.add(i); L.texts.add(l.from); L.texts.add(l.to); }
      for (const [idea] of n.ideas) { L.ideas.add(idea); L.spokes.push([idea, n.id]); }
    } else if (n.kind === 'part') {
      const t = n.text;
      L.texts.add(t.id);
      callout(t, n);
      for (const q of quotesOf(n)) {
        const other = textOf(q.other);
        L.texts.add(other.id);
        L.links.add(q.tie.link);
        // The passages of the other text that the same link quotes.
        for (const tie of G.ties) {
          if (tie.how !== 'quotes' || tie.link !== q.tie.link) continue;
          const p = byId.get(q.mine ? tie.to : tie.from);
          if (p && p.kind === 'part' && p.text.id === other.id) callout(other, p);
        }
      }
      for (const [idea] of namesOf(n)) { L.ideas.add(idea); L.spokes.push([idea, t.id]); }
      for (const o of sameAs(n)) { L.texts.add(o.text.id); callout(o.text, o); }
    } else {
      L.ideas.add(n.id);
      for (const [tid, count] of n.texts) { L.texts.add(tid); L.badges.set(tid, count); L.spokes.push([n.id, tid]); }
      G.links.forEach((l, i) => { if (n.texts.has(l.from) && n.texts.has(l.to)) L.links.add(i); });
    }
    return L;
  }

  // ---- Drawing -----------------------------------------------------------

  let queued = 0;
  function draw() { if (!queued) queued = requestAnimationFrame(() => { queued = 0; drawNow(); }); }
  const widths = new Map();
  function textWidth(font, s) {
    const k = font + '|' + s;
    let w = widths.get(k);
    if (w === undefined) { ctx.font = font; w = ctx.measureText(s).width; widths.set(k, w); }
    return w;
  }

  function drawNow() {
    if (!W) return;
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    ctx.fillStyle = rgba(C.sidebar);
    ctx.fillRect(0, 0, W, H);
    const focus = lit.main !== null;
    // The lanes, banded, each named at the left edge however far you pan.
    for (let i = 0; i < G.lanes.length; i++) {
      const y0 = sy(AXIS + i * LANE_H), y1 = sy(AXIS + (i + 1) * LANE_H);
      if (i % 2 === 0) { ctx.fillStyle = rgba(C.band); ctx.fillRect(0, y0, W, y1 - y0); }
    }
    ctx.fillStyle = rgba(C.band);
    ctx.fillRect(0, sy(ideasTop), W, sy(world.y1) - sy(ideasTop));
    // Years: faint lines, and their numbers along the top, kept in view.
    ctx.font = `11px ${FONT}`;
    ctx.textBaseline = 'middle';
    let lastTick = -1e9;
    const ticks = [];
    const tickY = Math.max(10, sy(AXIS / 2));
    for (const y of [1200, 1300, 1400, 1500, 1600, 1650, 1700, 1750, 1775, 1800, 1825, 1850, 1875, 1900, 1925, 1950, 1975, 2000]) {
      const x = sx(xOfYear(y));
      if (x < -20 || x > W + 20 || x - lastTick < 44) continue;
      lastTick = x;
      ticks.push([x, y]);
      ctx.strokeStyle = rgba(C.ink, C.dark ? 0.07 : 0.06);
      ctx.lineWidth = 1;
      ctx.beginPath(); ctx.moveTo(x, tickY + 8); ctx.lineTo(x, sy(world.y1)); ctx.stroke();
    }
    // The links, under everything else.
    G.links.forEach((l, i) => {
      const a = byId.get(l.from), b = byId.get(l.to);
      const s = HOW[l.how];
      const on = lit.links.has(i), pre = light.links.has(i);
      let alpha = focus ? (on ? 0.95 : 0.1) : 0.42;
      if (pre && !on) alpha = Math.max(alpha, 0.55);
      edge(a, b, rgba(C[s.c], alpha), on ? s.w + 1 : pre ? s.w + 0.4 : s.w, s.dash);
    });
    // The spokes from ideas to the texts that name them.
    const spoke = (L, alpha) => {
      ctx.setLineDash([]);
      ctx.lineWidth = 1;
      ctx.strokeStyle = rgba(C.frozen, alpha);
      ctx.beginPath();
      for (const [iid, tid] of L.spokes) {
        const i = byId.get(iid), t = byId.get(tid);
        const x0 = sx(i.x), y0 = sy(i.y), x1 = sx(t.x), y1 = sy(t.y);
        ctx.moveTo(x0, y0);
        ctx.bezierCurveTo(x0, y0 - (y0 - y1) * 0.5, x1, y1 + (y0 - y1) * 0.35, x1, y1);
      }
      ctx.stroke();
    };
    if (light !== lit) spoke(light, 0.22);
    spoke(lit, lit.main && lit.main.kind === 'idea' ? 0.5 : 0.28);
    // The texts and ideas.
    const labels = [];
    const scale = Math.min(1.6, Math.max(0.55, Math.sqrt(cam.k * 1.6)));
    for (const t of texts) {
      const x = sx(t.x), y = sy(t.y);
      const r = Math.max(3, t.r * scale);
      if (x < -60 || x > W + 60 || y < -60 || y > H + 60) continue;
      const on = lit.texts.has(t.id), pre = light.texts.has(t.id), main = lit.main === t || (lit.main && lit.main.kind === 'part' && lit.main.text === t);
      const dim = focus && !on && !pre;
      ctx.globalAlpha = dim ? 0.32 : 1;
      if (main) { ctx.fillStyle = rgba(C.accent, 0.18); ctx.beginPath(); ctx.arc(x, y, r + 7, 0, Math.PI * 2); ctx.fill(); }
      ctx.beginPath(); ctx.arc(x, y, r, 0, Math.PI * 2);
      ctx.fillStyle = rgba(main ? C.accent : on ? C['accent-fill'] : C.card);
      ctx.fill();
      ctx.lineWidth = main ? 2 : hover === t.id ? 2 : 1.2;
      ctx.strokeStyle = rgba(main ? C.ink : on || pre ? C.accent : C.muted, main ? 0.9 : 1);
      ctx.stroke();
      ctx.globalAlpha = 1;
      labels.push({ n: t, x, y, r, pri: main ? 1000 : hover === t.id ? 950 : on ? 600 + t.r : pre ? 500 : 100 + t.r * 4 + (linksOf.get(t.id) || []).length * 9, dim, font: `${main || on ? 600 : 500} 12px ${FONT}`, text: t.name });
      const badge = lit.badges.get(t.id);
      if (badge) {
        const s = String(badge), bw = Math.max(16, textWidth(`600 10px ${FONT}`, s) + 8);
        ctx.fillStyle = rgba(C.frozen);
        roundRect(x + r * 0.6, y - r - 12, bw, 15, 7.5); ctx.fill();
        ctx.fillStyle = rgba(C.card);
        ctx.font = `600 10px ${FONT}`; ctx.textAlign = 'center';
        ctx.fillText(s, x + r * 0.6 + bw / 2, y - r - 4.5);
      }
    }
    for (const i of ideas) {
      const x = sx(i.x), y = sy(i.y);
      if (x < -40 || x > W + 40 || y < -40 || y > H + 40) continue;
      const on = lit.ideas.has(i.id), pre = light.ideas.has(i.id), main = lit.main === i;
      const dim = focus && !on && !pre;
      const r = Math.max(3, i.r * scale) * (main ? 1.3 : 1);
      ctx.globalAlpha = dim ? 0.3 : 1;
      ctx.beginPath();
      ctx.moveTo(x, y - r); ctx.lineTo(x + r, y); ctx.lineTo(x, y + r); ctx.lineTo(x - r, y); ctx.closePath();
      ctx.fillStyle = rgba(main ? C.frozen : on ? mix(C.card, C.frozen, 0.45) : C.panel);
      ctx.fill();
      ctx.lineWidth = main || hover === i.id ? 2 : 1.1;
      ctx.strokeStyle = rgba(main ? C.ink : on || pre ? C.frozen : C.muted);
      ctx.stroke();
      ctx.globalAlpha = 1;
      labels.push({ n: i, x, y, r, pri: main ? 990 : hover === i.id ? 940 : on ? 560 : pre ? 480 : 60 + i.texts.size, dim, font: `${main ? 600 : 400} 11.5px ${FONT}`, text: i.name });
    }
    // Labels: the most important first, each where it overlaps none of
    // the dots, the lanes' names or the labels placed before it.
    labels.sort((a, b) => b.pri - a.pri);
    // The dots: a lit label may cover a dot the pick dims, never a lit one.
    const dots = labels.map(L => [L.x - L.r - 1, L.y - L.r - 1, L.x + L.r + 1, L.y + L.r + 1, L.dim]);
    const placed = [];
    ctx.font = `600 11px ${FONT}`;
    const lx = Math.max(10, sx(world.x0) + 8);
    const laneY = i => sy(AXIS + i * LANE_H) + 8;
    G.lanes.forEach((l, i) => placed.push([lx - 2, laneY(i) - 2, lx + textWidth(`600 11px ${FONT}`, l.name) + 4, laneY(i) + 14]));
    for (const [x, y] of ticks) { const w = textWidth(`11px ${FONT}`, String(y)); placed.push([x - w / 2 - 3, tickY - 7, x + w / 2 + 3, tickY + 7]); }
    // The parts a pick names go in pills by their texts, placed after the
    // lit labels and before the rest, so the rest make way for them.
    const pills = [];
    let pilled = false;
    const placePillsOnce = () => {
      if (pilled) return;
      pilled = true;
      for (const [tid, parts] of lit.callouts) pills.push(placePills(byId.get(tid), parts, placed, dots, false));
      if (light !== lit) for (const [tid, parts] of light.callouts) if (!lit.callouts.has(tid)) pills.push(placePills(byId.get(tid), parts, placed, dots, true));
    };
    placed.push([lx - 2, sy(ideasTop) + 6, lx + textWidth(`600 11px ${FONT}`, 'Rights and ideas they name') + 4, sy(ideasTop) + 22]);
    const apart = (x0, y0, x1, y1) => p => x1 < p[0] || x0 > p[2] || y1 < p[1] || y0 > p[3];
    let litLabel = false;
    const free = (x0, y0, x1, y1) => placed.every(apart(x0, y0, x1, y1)) && dots.every(d => (litLabel && d[4]) || apart(x0, y0, x1, y1)(d));
    ctx.textBaseline = 'middle';
    for (const L of labels) {
      if (L.pri < 480) placePillsOnce();
      litLabel = L.pri >= 480;
      const w = textWidth(L.font, L.text);
      const h = 14;
      const sides = [[L.x + L.r + 5, L.y - h / 2], [L.x - L.r - 5 - w, L.y - h / 2], [L.x - w / 2, L.y + L.r + 3], [L.x - w / 2, L.y - L.r - 3 - h]];
      // A lit label may also stand a little further off.
      if (L.pri >= 480) sides.push([L.x + L.r + 5, L.y - h * 1.6], [L.x + L.r + 5, L.y + h * 0.6], [L.x - L.r - 5 - w, L.y - h * 1.6], [L.x - L.r - 5 - w, L.y + h * 0.6]);
      let spot = null;
      for (const [x0, y0] of sides) if (free(x0 - 2, y0, x0 + w + 2, y0 + h)) { spot = [x0, y0]; break; }
      const must = L.pri >= 900;
      if (!spot) { if (!must) continue; spot = sides[0]; }
      if (!must && (spot[0] < 0 || spot[0] + w > W || spot[1] < 0 || spot[1] + h > H)) continue;
      placed.push([spot[0] - 2, spot[1], spot[0] + w + 2, spot[1] + h]);
      if (must || L.pri >= 480) { ctx.fillStyle = rgba(C.card, 0.86); roundRect(spot[0] - 4, spot[1] - 1, w + 8, h + 2, 4); ctx.fill(); }
      ctx.font = L.font;
      ctx.textAlign = 'left';
      ctx.fillStyle = rgba(L.dim ? C.muted : C.ink, L.dim ? 0.6 : 1);
      ctx.fillText(L.text, spot[0], spot[1] + h / 2 + 0.5);
    }
    placePillsOnce();
    for (const p of pills) drawPills(p);
    // The years' numbers, and the lanes' names last, over the bands at the
    // left edge.
    ctx.font = `11px ${FONT}`;
    ctx.textAlign = 'center';
    ctx.textBaseline = 'middle';
    for (const [x, y] of ticks) {
      const w = textWidth(ctx.font, String(y));
      ctx.fillStyle = rgba(C.sidebar, 0.85);
      roundRect(x - w / 2 - 4, tickY - 8, w + 8, 16, 4); ctx.fill();
      ctx.fillStyle = rgba(C.muted);
      ctx.fillText(String(y), x, tickY);
    }
    ctx.font = `600 11px ${FONT}`;
    ctx.textAlign = 'left';
    ctx.textBaseline = 'top';
    G.lanes.forEach((l, i) => {
      const y = laneY(i);
      if (y < -20 || y > H) return;
      ctx.fillStyle = rgba(i % 2 === 0 ? C.band : C.sidebar, 0.85);
      roundRect(lx - 4, y - 2, textWidth(ctx.font, l.name) + 8, 16, 4); ctx.fill();
      ctx.fillStyle = rgba(C.muted, 0.95);
      ctx.fillText(l.name, lx, y);
    });
    const iy = sy(ideasTop) + 8;
    if (iy > -20 && iy < H) { ctx.fillStyle = rgba(C.muted, 0.9); ctx.fillText('Rights and ideas they name', lx, iy); }
  }

  function roundRect(x, y, w, h, r) {
    ctx.beginPath();
    ctx.moveTo(x + r, y); ctx.arcTo(x + w, y, x + w, y + h, r); ctx.arcTo(x + w, y + h, x, y + h, r);
    ctx.arcTo(x, y + h, x, y, r); ctx.arcTo(x, y, x + w, y, r); ctx.closePath();
  }

  // edge draws a link from the later text back to the earlier: an arc over
  // a lane, or an S between lanes.
  function edge(a, b, colour, width, dash) {
    const x0 = sx(a.x), y0 = sy(a.y), x1 = sx(b.x), y1 = sy(b.y);
    if (Math.max(x0, x1) < -50 || Math.min(x0, x1) > W + 50) return;
    ctx.strokeStyle = colour;
    ctx.lineWidth = width;
    ctx.setLineDash(dash);
    ctx.lineCap = 'round';
    ctx.beginPath();
    ctx.moveTo(x0, y0);
    if (Math.abs(a.y - b.y) < 40) {
      const h = Math.min(90, Math.max(18, Math.abs(a.x - b.x) * 0.22)) * cam.k;
      ctx.bezierCurveTo(x0, y0 - h, x1, y1 - h, x1, y1);
    } else {
      const m = (x0 - x1) * 0.45;
      ctx.bezierCurveTo(x0 - m, y0, x1 + m, y1, x1, y1);
    }
    ctx.stroke();
    ctx.setLineDash([]);
  }

  // placePills finds room for a text's pills, the parts a pick names: under
  // the text, over it, beside it, or further down, wherever they overlap no
  // other pills and no dots.
  function placePills(t, parts, placed, dots, faint) {
    const scale = Math.min(1.6, Math.max(0.55, Math.sqrt(cam.k * 1.6)));
    const x = sx(t.x), y = sy(t.y), r = Math.max(3, t.r * scale);
    const font = `600 11.5px ${FONT}`;
    const items = parts.slice(0, 4).map((p, i) => (parts.length > 4 && i === 3 ? `${label(p)} and ${parts.length - 3} more` : label(p)));
    const ws = items.map(s => textWidth(font, s) + 14);
    const w = Math.max(...ws), h = items.length * 22 - 4;
    const spots = [[x - w / 2, y + r + 6], [x - w / 2, y - r - 6 - h], [x + r + 8, y - h / 2], [x - r - 8 - w, y - h / 2]];
    for (let d = 1; d <= 4; d++) spots.push([x - w / 2, y + r + 6 + d * 24], [x - w / 2, y - r - 6 - h - d * 24]);
    const clear = ([x0, y0]) => [...placed, ...dots.filter(d => !d[4])].every(p => x0 + w < p[0] || x0 > p[2] || y0 + h < p[1] || y0 > p[3]);
    const [x0, y0] = spots.find(clear) || spots[0];
    placed.push([x0 - 2, y0 - 2, x0 + w + 2, y0 + h + 2]);
    return { x0, y0, w, items, ws, faint, font, tx: x, ty: y };
  }

  function drawPills(p) {
    ctx.globalAlpha = p.faint ? 0.75 : 1;
    // A thin stem from the text to its pills when they sit away from it.
    const cy = Math.max(p.y0, Math.min(p.ty, p.y0 + p.items.length * 22 - 4));
    const cx = Math.max(p.x0, Math.min(p.tx, p.x0 + p.w));
    if (Math.hypot(cx - p.tx, cy - p.ty) > 26) {
      ctx.strokeStyle = rgba(p.faint ? C.muted : C.accent, 0.7);
      ctx.lineWidth = 1;
      ctx.beginPath(); ctx.moveTo(p.tx, p.ty); ctx.lineTo(cx, cy); ctx.stroke();
    }
    let y = p.y0;
    p.items.forEach((s, i) => {
      const w = p.ws[i], x = p.x0 + (p.w - w) / 2;
      ctx.fillStyle = rgba(C.card);
      ctx.strokeStyle = rgba(p.faint ? C.muted : C.accent);
      ctx.lineWidth = 1.2;
      roundRect(x, y, w, 18, 9); ctx.fill(); ctx.stroke();
      ctx.fillStyle = rgba(C.ink);
      ctx.font = p.font; ctx.textAlign = 'center'; ctx.textBaseline = 'middle';
      ctx.fillText(s, x + w / 2, y + 9.5);
      y += 22;
    });
    ctx.globalAlpha = 1;
  }

  // ---- Picking on the map: the nearest node, within reach --------------

  function nearest(px, py, reach = 14) {
    let best = null, bd = reach;
    const scale = Math.min(1.6, Math.max(0.55, Math.sqrt(cam.k * 1.6)));
    for (const n of [...texts, ...ideas]) {
      const d = Math.hypot(sx(n.x) - px, sy(n.y) - py) - Math.max(3, n.r * scale);
      if (d < bd) { bd = d; best = n; }
    }
    return best;
  }

  // ---- The pick, the address and the trail ------------------------------

  const trail = [];
  let pending = false;
  function pick(id, opts = {}) {
    if (id && !byId.has(id)) id = null;
    const changed = id !== current;
    current = id;
    lit = lights(id);
    if (id) { const i = trail.indexOf(id); if (i >= 0) trail.splice(i, 1); trail.push(id); if (trail.length > 8) trail.shift(); }
    renderTrail();
    if (changed || opts.render) renderCard(opts);
    if (changed && opts.push !== false && !pending) {
      pending = true;
      queueMicrotask(() => {
        pending = false;
        const url = current ? '#' + current : location.pathname + location.search;
        try { history.pushState({ id: current }, '', url); } catch (_) { /* a page with no history keeps its place */ }
      });
    }
    if (opts.fly) frame(id);
    draw();
  }
  const fromHash = () => decodeURIComponent(location.hash.replace(/^#/, '')) || null;

  // frame brings a pick and what it lights into view, unless it is in view
  // already, so the camera moves only when there is something to see.
  function frame(id) {
    const n = id && byId.get(id);
    if (!n) return;
    const pts = [];
    const add = m => m && pts.push(m);
    if (n.kind === 'part') add(n.text); else add(n);
    for (const t of lit.texts) add(byId.get(t));
    if (n.kind === 'idea') for (const t of lit.texts) add(byId.get(t));
    const box = { x0: Infinity, y0: Infinity, x1: -Infinity, y1: -Infinity };
    for (const p of pts) { box.x0 = Math.min(box.x0, p.x - 70); box.x1 = Math.max(box.x1, p.x + 140); box.y0 = Math.min(box.y0, p.y - 40); box.y1 = Math.max(box.y1, p.y + 50); }
    const inView = pts.every(p => { const x = sx(p.x), y = sy(p.y); return x > 30 && x < W - 30 && y > 30 && y < H - 30; });
    const main = n.kind === 'part' ? n.text : n;
    const v = fitView(box, 40);
    v.k = Math.min(v.k, fitK * 4);
    if (pts.length === 1) { v.x = main.x; v.y = main.y; v.k = Math.max(cam.k, fitK * 2.2); }
    // In view, and no closer for flying: stay, so the camera moves only
    // when there is something to see.
    if (inView && v.k < cam.k * 1.4) return;
    go(v);
  }

  // ---- The card ---------------------------------------------------------

  const card = document.getElementById('card');
  const esc = s => String(s).replace(/[&<>"]/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' })[c]);
  const fmtDate = when => {
    const [y, m, d] = when.split('-').map(Number);
    if (!m) return String(y);
    return new Date(Date.UTC(y, m - 1, d)).toLocaleDateString('en-GB', { day: 'numeric', month: 'long', year: 'numeric', timeZone: 'UTC' });
  };
  const laneName = id => G.lanes.find(l => l.id === id).name;
  const kindWord = k => ({ charter: 'charter', statute: 'statute', declaration: 'declaration', articles: 'articles', ordinance: 'ordinance', constitution: 'constitution', amendments: 'amendments', proclamation: 'proclamation', address: 'address', covenant: 'covenant', convention: 'convention' })[k] || k;
  // why gives a link's sentence with its quotations set as quotations.
  const why = s => esc(s).replace(/“([^”]*)”/g, '<q>$1</q>');
  // inline gives a passage's words with **bold** kept and marks lit: each
  // mark is [start, end, class] in the passage's UTF-16 places.
  function inline(text, marks = []) {
    const at = [];
    for (const [s, e, cls] of marks) { at.push([s, 1, cls]); at.push([e, 0, cls]); }
    at.sort((a, b) => a[0] - b[0] || a[1] - b[1]);
    let out = '', i = 0, bold = false, open = 0, j = 0;
    while (i <= text.length) {
      while (j < at.length && at[j][0] === i) {
        if (at[j][1]) { if (!open++) out += `<mark class="${at[j][2] || ''}">`; } else if (open && !--open) out += '</mark>';
        j++;
      }
      if (i === text.length) break;
      if (text.startsWith('**', i)) { out += bold ? '</b>' : '<b>'; bold = !bold; i += 2; continue; }
      const next = Math.min(...[text.indexOf('**', i) === -1 ? text.length : text.indexOf('**', i), j < at.length ? at[j][0] : text.length]);
      out += esc(text.slice(i, Math.max(next, i + 1)));
      i = Math.max(next, i + 1);
    }
    if (open) out += '</mark>';
    if (bold) out += '</b>';
    return out;
  }
  // words gives a run of a text's blocks, with the marks each block takes.
  function words(doc, from, to, marks = new Map(), cap = 90) {
    const d = docOf.get(doc);
    let html = '';
    const end = Math.min(to, from + cap);
    for (let i = from; i < end; i++) {
      const b = d.blocks[i];
      const m = marks.get(i) || [];
      if (b.kind === 'h2' || b.kind === 'h3' || b.kind === 'h4') html += `<div class="h" data-block="${i}">${inline(b.text)}</div>`;
      else if (b.kind === 'note') html += `<p class="note" data-block="${i}">${inline(b.text)}</p>`;
      else if (b.kind === 'list') html += `<ul data-block="${i}">${b.items.map(x => `<li>${inline(x)}</li>`).join('')}</ul>`;
      else html += `<p data-block="${i}">${inline(b.text, m)}</p>`;
    }
    if (end < to) html += `<button class="more" data-more="${doc}:${end}:${to}">Show the rest, ${to - end} more passages</button>`;
    return `<div class="words">${html}</div>`;
  }
  // The marks of a node's quotations, by block, for the words it shows.
  function quoteMarks(n) {
    const m = new Map();
    const add = (bi, s, e, cls) => { const a = m.get(bi) || []; a.push([s, e, cls]); m.set(bi, a); };
    const doc = n.kind === 'text' ? n.id : n.doc;
    for (const t of G.ties) {
      if (t.how !== 'quotes') continue;
      const p = byId.get(t.from).kind === 'part' ? byId.get(t.from) : byId.get(t.to);
      if (p.doc !== doc) continue;
      for (const [bi, s, e] of t.marks) add(bi, s, e, 'quote');
    }
    return m;
  }
  const chip = (idea, n) => `<button class="chip" data-pick="${esc(idea.id)}"><span class="dot" aria-hidden="true"></span>${esc(idea.name)}${n ? ` <span class="n">${n}</span>` : ''}</button>`;
  function ideaChips(m) {
    const list = [...m].map(([id, n]) => [byId.get(id), n]).sort((a, b) => b[1] - a[1] || a[0].name.localeCompare(b[0].name));
    return `<div class="chips">${list.map(([i, n]) => chip(i, n)).join('')}</div>`;
  }
  function linkRow(i, side) {
    const l = G.links[i];
    const other = byId.get(side === 'out' ? l.to : l.from);
    const s = HOW[l.how];
    const head = side === 'out' ? `${esc(s.say[0].toUpperCase() + s.say.slice(1))} <b>${esc(other.name)}</b>` : `<b>${esc(other.name)}</b> ${esc(s.say)} it`;
    return `<button class="link" data-pick="${esc(other.id)}" data-hover="${esc(other.id)}">${head}<span class="how">${howSample(l.how)}${esc(other.year)}</span><span class="why">${why(l.why)}</span></button>`;
  }

  function renderCard(opts = {}) {
    const n = current && byId.get(current);
    let html;
    if (!n) html = overviewCard();
    else if (n.kind === 'text') html = textCard(n);
    else if (n.kind === 'part') html = partCard(n);
    else html = ideaCard(n);
    card.innerHTML = html;
    for (const tree of card.querySelectorAll('[data-tree]')) buildTree(tree);
    card.scrollTop = 0;
    if (opts.block !== undefined) {
      const el = card.querySelector(`[data-block="${opts.block}"]`);
      if (el) {
        if (opts.find) el.innerHTML = inline(docOf.get(n.kind === 'text' ? n.id : n.doc).blocks[opts.block].text, opts.find.map(([s, e]) => [s, e, '']));
        el.classList.add('lit');
        el.scrollIntoView({ block: 'center' });
      }
    }
  }

  function overviewCard() {
    const fams = G.families.map(f => {
      const list = ideas.filter(i => i.family === f.id).sort((a, b) => b.texts.size - a.texts.size);
      return `<div class="fam">${esc(f.name)}</div><div class="chips">${list.map(i => chip(i, i.texts.size)).join('')}</div>`;
    }).join('');
    const first = byDate[0], last = byDate[byDate.length - 1];
    return `<h1>The charters</h1>
      <p class="lead">${texts.length} texts of rights, from ${esc(first.name)} in ${first.year} to the ${esc(last.name)} in ${last.year}, laid out by when and where they were made. Each line between two texts is a link: how the later one drew on the earlier, quoting both.</p>
      <p class="muted">Pick a text, a part of one or a right to see how it bears on the rest, or search for any word, in any spelling.</p>
      <h2>Rights and ideas, and how many texts name each</h2>${fams}
      <h2>The texts, oldest first</h2><ul class="tree" role="tree" aria-label="The texts" data-tree="*"></ul>
      <p class="about" style="margin-top:18px">Each text’s card says where its words come from, word for word, and on what terms. The United Kingdom’s texts contain public sector information licensed under the Open Government Licence v3.0; Sweden’s Press Act is Ian Giles and Peter Graves’s translation, under CC BY 4.0. The links, the parts and the rights named are this map’s reading of the texts, each shown with the words it rests on.</p>`;
  }

  function textCard(t) {
    const out = (linksOf.get(t.id) || []).filter(i => G.links[i].from === t.id);
    const into = (linksOf.get(t.id) || []).filter(i => G.links[i].to === t.id);
    const parts = kids.get(t.id) || [];
    const d = docOf.get(t.id);
    const firstPart = parts.length ? parts[0].span[0] : d.blocks.length;
    let html = `<h1>${esc(t.title)}</h1>
      <div class="meta">${esc(fmtDate(t.when))} · ${esc(kindWord(t.dkind))} · ${esc(laneName(t.lane))}</div>
      <p class="about">${inline(t.about)}</p>`;
    if (out.length) html += `<h2>It drew on</h2>${out.map(i => linkRow(i, 'out')).join('')}`;
    if (into.length) html += `<h2>Later texts that drew on it</h2>${into.map(i => linkRow(i, 'in')).join('')}`;
    if (t.ideas.size) html += `<h2>Rights and ideas it names, and in how many passages</h2>${ideaChips(t.ideas)}`;
    if (parts.length) html += `<h2>Its parts</h2><ul class="tree" role="tree" aria-label="Parts of the ${esc(t.name)}" data-tree="${esc(t.id)}"></ul>`;
    if (firstPart > 0) html += `<h2>${parts.length ? 'Its opening words' : 'Its words'}</h2>${words(t.id, 0, firstPart, quoteMarks(t), parts.length ? 40 : 400)}`;
    html += `<p class="about" style="margin-top:18px">SHA-512 <code>${esc(t.sha.slice(0, 16))}…</code>, the fingerprint the Charters chain keeps.</p>`;
    return html;
  }

  function pathOf(n) {
    const out = [];
    for (let up = byId.get(n.up); up; up = up.kind === 'part' ? byId.get(up.up) : null) out.unshift(up);
    return out;
  }

  function partCard(p) {
    const t = p.text;
    const path = pathOf(p).map(u => `<button data-pick="${esc(u.id)}">${esc(label(u))}</button>`).join('<span aria-hidden="true">›</span>');
    const qs = quotesOf(p);
    const sub = kids.get(p.id) || [];
    let html = `<nav class="path" aria-label="Where it is">${path}</nav>
      <h1>${esc(p.name)}</h1>
      <div class="meta">${esc(t.title)}, ${esc(t.year)}</div>`;
    for (const o of sameAs(p)) html += `<button class="link" data-pick="${esc(o.id)}" data-hover="${esc(o.text.id)}">${o.exact ? 'The very same words as' : 'Word for word, spelling and stops aside, as'} <b>${esc(label(o))}</b> in the ${esc(o.text.name)}, ${esc(o.text.year)}</button>`;
    if (qs.length) {
      html += '<h2>Links that quote it</h2>';
      const seen = new Set();
      for (const q of qs) {
        if (seen.has(q.tie.link)) continue;
        seen.add(q.tie.link);
        const l = G.links[q.tie.link];
        const other = textOf(q.other);
        const s = HOW[l.how];
        const head = q.mine ? `${esc(s.say[0].toUpperCase() + s.say.slice(1))} <b>${esc(other.name)}</b>` : `<b>${esc(other.name)}</b> ${esc(s.say)} it`;
        const there = [...(lit.callouts.get(other.id) || [])].map(x => `<button class="chip" data-pick="${esc(x.id)}">${esc(label(x))}</button>`).join('');
        html += `<div class="link" data-hover="${esc(other.id)}"><button class="row" data-pick="${esc(other.id)}" style="margin:0;width:100%">${head}<span class="how">${howSample(l.how)}${esc(other.year)}</span></button><span class="why">${why(l.why)}</span>${there ? `<div class="chips" style="margin-top:6px">${there}</div>` : ''}</div>`;
      }
    }
    const names = namesOf(p);
    if (names.size) html += `<h2>Rights and ideas it names</h2>${ideaChips(names)}`;
    if (sub.length) html += `<h2>Its parts</h2><ul class="tree" role="tree" aria-label="Parts of ${esc(p.name)}" data-tree="${esc(p.id)}"></ul>`;
    html += `<h2>Its words</h2>${words(p.doc, p.span[0], p.span[1], quoteMarks(p))}`;
    return html;
  }

  // snippet gives the words around a mark, cut at words.
  function snippet(text, s, e, room = 90) {
    let a = Math.max(0, s - room), b = Math.min(text.length, e + room);
    if (a > 0) a = text.indexOf(' ', a) + 1 || a;
    if (b < text.length) { const sp = text.lastIndexOf(' ', b); if (sp > e) b = sp; }
    const plain = x => esc(x.replace(/\*\*/g, ''));
    return `${a > 0 ? '…' : ''}${plain(text.slice(a, s))}<mark>${plain(text.slice(s, e))}</mark>${plain(text.slice(e, b))}${b < text.length ? '…' : ''}`;
  }

  function ideaCard(i) {
    const fam = families.get(i.family);
    const uses = i.uses.slice().sort((a, b) => {
      const ta = textOf(byId.get(a.from)), tb = textOf(byId.get(b.from));
      return ta.t - tb.t || a.marks[0][0] - b.marks[0][0];
    });
    const n = uses.length;
    let html = `<h1>${esc(i.name)}</h1>
      <div class="meta">${esc(fam.name)} · named in ${n} ${n === 1 ? 'passage' : 'passages'} of ${i.texts.size} ${i.texts.size === 1 ? 'text' : 'texts'}, ${i.first === i.last ? i.first : `${i.first} to ${i.last}`}</div>
      <p class="about">Found where these words are, in any spelling: ${i.words.map(w => `“${esc(w)}”`).join(', ')}.</p>
      <h2>Where it is written, oldest first</h2>`;
    let lastText = null;
    for (const u of uses) {
      const from = byId.get(u.from);
      const t = textOf(from);
      if (t !== lastText) {
        html += `<div class="fam" style="margin-top:14px"><button class="chip" data-pick="${esc(t.id)}" data-hover="${esc(t.id)}">${esc(t.name)} <span class="n">${t.year}</span></button></div>`;
        lastText = t;
      }
      const [bi, s, e] = u.marks[0];
      const text = docOf.get(t.id).blocks[bi].text;
      const where = from.kind === 'part' ? label(from) : 'Its words';
      html += `<button class="row" data-pick="${esc(from.id)}" data-block="${bi}" data-find="${s},${e}" data-hover="${esc(t.id)}"><b>${esc(where)}</b><span class="snip">${snippet(text, s, e)}</span></button>`;
    }
    return html;
  }

  // Clicks in the card pick what they name; pointing at one lights it.
  card.addEventListener('click', e => {
    const more = e.target.closest('[data-more]');
    if (more) {
      const [doc, from, to] = more.dataset.more.split(':');
      more.outerHTML = words(doc, +from, +to, quoteMarks(byId.get(current)), 1e9).replace(/^<div class="words">|<\/div>$/g, '');
      return;
    }
    const b = e.target.closest('[data-pick]');
    if (!b || b.closest('[role="tree"]')) return;
    const opts = { fly: true };
    if (b.dataset.block) { opts.block = +b.dataset.block; if (b.dataset.find) opts.find = [b.dataset.find.split(',').map(Number)]; opts.render = true; }
    pick(b.dataset.pick, opts);
  });
  card.addEventListener('pointerover', e => {
    const b = e.target.closest('[data-hover],[data-pick]');
    const id = b ? b.dataset.hover || b.dataset.pick : null;
    setLight(id && id !== current ? id : null);
  });
  card.addEventListener('pointerleave', () => setLight(null));
  function setLight(id) {
    if (id === (light.main && light.main.id)) return;
    light = id ? lights(id) : lit;
    hover = id;
    draw();
  }

  // ---- The tree of parts: WAI-ARIA's tree view --------------------------

  let open = new Set();
  const chev = '<svg viewBox="0 0 10 10" aria-hidden="true"><path d="M3 1.5 7.5 5 3 8.5z"/></svg>';
  function treeItem(n, level, pos, size) {
    const sub = kids.get(n.id) || [];
    const li = document.createElement('li');
    li.setAttribute('role', 'treeitem');
    li.setAttribute('aria-level', level);
    li.setAttribute('aria-posinset', pos);
    li.setAttribute('aria-setsize', size);
    li.setAttribute('aria-selected', String(n.id === current));
    li.tabIndex = -1;
    li.dataset.id = n.id;
    const count = n.kind === 'text' ? n.year : sub.length ? sub.length : '';
    li.innerHTML = `<div class="item" aria-selected="${n.id === current}"><button class="caret${sub.length ? '' : ' leaf'}" tabindex="-1" aria-hidden="true">${chev}</button><span class="label">${esc(n.kind === 'text' ? n.name : n.name.replace(/\*\*/g, ''))}</span><span class="n">${count}</span></div>`;
    if (sub.length) {
      li.setAttribute('aria-expanded', String(open.has(n.id)));
      if (open.has(n.id)) li.appendChild(group(sub, level + 1));
    }
    return li;
  }
  function group(list, level) {
    const ul = document.createElement('ul');
    ul.setAttribute('role', 'group');
    list.forEach((n, i) => ul.appendChild(treeItem(n, level, i + 1, list.length)));
    return ul;
  }
  function buildTree(ul) {
    const root = ul.dataset.tree;
    const list = root === '*' ? byDate : kids.get(root) || [];
    list.forEach((n, i) => ul.appendChild(treeItem(n, 1, i + 1, list.length)));
    const items = ul.querySelectorAll('[role="treeitem"]');
    const first = ul.querySelector('[role="treeitem"][aria-selected="true"]') || items[0];
    if (first) first.tabIndex = 0;
    ul.addEventListener('click', e => {
      const li = e.target.closest('[role="treeitem"]');
      if (!li) return;
      focusItem(ul, li);
      if (e.target.closest('.caret') || e.altKey) toggle(li, e.altKey);
      else pick(li.dataset.id === current ? (byId.get(current).up || null) : li.dataset.id, { fly: true });
    });
    ul.addEventListener('keydown', e => treeKey(ul, e));
    ul.addEventListener('pointerover', e => { const li = e.target.closest('[role="treeitem"]'); if (li) setLight(li.dataset.id !== current ? li.dataset.id : null); });
  }
  function visible(ul) { return [...ul.querySelectorAll('[role="treeitem"]')].filter(li => !li.parentElement.closest('[role="treeitem"][aria-expanded="false"]')); }
  function focusItem(ul, li) {
    for (const x of ul.querySelectorAll('[role="treeitem"][tabindex="0"]')) x.tabIndex = -1;
    li.tabIndex = 0;
    li.focus({ preventScroll: true });
    li.firstElementChild.scrollIntoView({ block: 'nearest' });
  }
  function toggle(li, all, to) {
    const id = li.dataset.id;
    if (!li.hasAttribute('aria-expanded')) return;
    const want = to === undefined ? li.getAttribute('aria-expanded') !== 'true' : to;
    if (want) open.add(id); else open.delete(id);
    li.setAttribute('aria-expanded', String(want));
    if (want && !li.querySelector(':scope > ul')) li.appendChild(group(kids.get(id) || [], +li.getAttribute('aria-level') + 1));
    if (all && want) for (const k of li.querySelectorAll(':scope > ul > [role="treeitem"][aria-expanded]')) toggle(k, true, true);
    save();
  }
  let typed = '', typedAt = 0;
  function treeKey(ul, e) {
    const li = e.target.closest('[role="treeitem"]');
    if (!li) return;
    const rows = visible(ul);
    const i = rows.indexOf(li);
    const go1 = j => { if (rows[j]) focusItem(ul, rows[j]); };
    const expanded = li.getAttribute('aria-expanded');
    switch (e.key) {
      case 'ArrowDown': go1(i + 1); break;
      case 'ArrowUp': go1(i - 1); break;
      case 'Home': go1(0); break;
      case 'End': go1(rows.length - 1); break;
      case 'ArrowRight':
        if (expanded === 'false') toggle(li, e.altKey, true);
        else if (expanded === 'true') { const c = li.querySelector(':scope > ul > [role="treeitem"]'); if (c) focusItem(ul, c); }
        break;
      case 'ArrowLeft':
        if (expanded === 'true') toggle(li, false, false);
        else { const up = li.parentElement.closest('[role="treeitem"]'); if (up) focusItem(ul, up); }
        break;
      case 'Enter': case ' ': pick(li.dataset.id, { fly: true }); break;
      case '*': for (const s of li.parentElement.querySelectorAll(':scope > [role="treeitem"][aria-expanded="false"]')) toggle(s, false, true); break;
      default:
        if (e.key.length === 1 && !e.metaKey && !e.ctrlKey) {
          const now = performance.now();
          typed = now - typedAt > 700 ? e.key.toLowerCase() : typed + e.key.toLowerCase();
          typedAt = now;
          const from = rows.slice(i + (typed.length === 1 ? 1 : 0)).concat(rows);
          const hit = from.find(r => r.querySelector('.label').textContent.toLowerCase().startsWith(typed));
          if (hit) focusItem(ul, hit);
        } else return;
    }
    e.preventDefault();
    e.stopPropagation();
  }

  // ---- Search -----------------------------------------------------------

  const q = document.getElementById('q');
  const results = document.getElementById('results');
  let folded = null; // every passage's words, made at the first search
  let found = [];
  let active = -1;
  function passages() {
    if (folded) return folded;
    folded = [];
    for (const d of G.docs) d.blocks.forEach((b, bi) => {
      if (b.kind !== 'text') return;
      const ws = spans(b.text);
      folded.push({ doc: d.id, bi, ws, line: ' ' + ws.map(x => x.w).join(' ') + ' ' });
    });
    return folded;
  }
  const starts = (name, words) => { const ws = fold(name).split(' '); return words.every(w => ws.some(x => x.startsWith(w))); };
  function search(text) {
    const f = fold(text);
    found = [];
    active = -1;
    if (!f) { results.hidden = true; q.setAttribute('aria-expanded', 'false'); return; }
    const words = f.split(' ');
    const sections = [];
    const add = (title, rows) => { if (rows.length) sections.push([title, rows]); };
    add('Rights and ideas', ideas.filter(i => starts(i.name, words) || i.words.some(w => (' ' + w + ' ').includes(' ' + f + ' '))).slice(0, 6)
      .map(i => ({ id: i.id, html: `<span class="what">${esc(i.name)}</span><span class="where">${i.texts.size} texts, ${i.first} to ${i.last}</span>` })));
    add('Texts', texts.filter(t => starts(t.name, words) || starts(t.title, words)).slice(0, 6)
      .map(t => ({ id: t.id, html: `<span class="what">${esc(t.title)}</span><span class="where">${t.year} · ${esc(laneName(t.lane))}</span>` })));
    const parts = [];
    for (const n of G.nodes) if (n.kind === 'part' && starts(n.name, words) && parts.length < 8) parts.push(n);
    add('Parts', parts.map(p => ({ id: p.id, html: `<span class="what">${esc(label(p))}</span><span class="where">${esc(p.text.name)} · ${esc(pathOf(p).slice(1).map(label).join(' › ') || p.text.year)}</span>` })));
    const hits = [];
    const pad = ' ' + f + ' ';
    let more = 0;
    for (const p of passages()) {
      const at = p.line.indexOf(pad);
      if (at < 0) continue;
      if (hits.length >= 40) { more++; continue; }
      const before = p.line.slice(0, at + 1).trim();
      const k = before ? before.split(' ').length : 0;
      const s = p.ws[k].s, e = p.ws[k + words.length - 1].e;
      const part = byId.get(partAt.get(p.doc)[p.bi]);
      const text = docOf.get(p.doc).blocks[p.bi].text;
      hits.push({ id: part.id, block: p.bi, find: [[s, e]], html: `<span class="where">${esc(byId.get(p.doc).name)}${part.kind === 'part' ? ' · ' + esc(label(part)) : ''}</span><span class="snip">${snippet(text, s, e, 70)}</span>` });
    }
    add(hits.length ? `Passages${more ? `, the first 40 of ${40 + more}` : ''}` : 'Passages', hits);
    let html = '';
    for (const [title, rows] of sections) {
      html += `<h3>${esc(title)}</h3>`;
      for (const r of rows) { r.n = found.length; found.push(r); html += `<button class="result" role="option" id="r${r.n}" aria-selected="false" data-n="${r.n}">${r.html}</button>`; }
    }
    results.innerHTML = html || `<div class="none">Nothing found for “${esc(text.trim())}”. Try another word or spelling.</div>`;
    results.hidden = false;
    q.setAttribute('aria-expanded', 'true');
    setActive(found.length ? 0 : -1);
  }
  function setActive(i) {
    if (active >= 0) results.querySelector(`#r${active}`)?.setAttribute('aria-selected', 'false');
    active = i;
    if (i >= 0) {
      const el = results.querySelector(`#r${i}`);
      el.setAttribute('aria-selected', 'true');
      el.scrollIntoView({ block: 'nearest' });
      q.setAttribute('aria-activedescendant', `r${i}`);
      setLight(found[i].id);
    } else q.removeAttribute('aria-activedescendant');
  }
  function choose(i) {
    const r = found[i];
    if (!r) return;
    closeResults();
    pick(r.id, { fly: true, block: r.block, find: r.find, render: true });
  }
  function closeResults() { results.hidden = true; q.setAttribute('aria-expanded', 'false'); setLight(null); }
  let searchTimer = 0;
  q.addEventListener('input', () => { clearTimeout(searchTimer); searchTimer = setTimeout(() => search(q.value), 90); });
  q.addEventListener('focus', () => { if (q.value.trim()) search(q.value); });
  q.addEventListener('keydown', e => {
    if (e.key === 'ArrowDown') { setActive(Math.min(found.length - 1, active + 1)); e.preventDefault(); }
    else if (e.key === 'ArrowUp') { setActive(Math.max(0, active - 1)); e.preventDefault(); }
    else if (e.key === 'Enter') { choose(active); e.preventDefault(); }
    else if (e.key === 'Escape') { if (!results.hidden) closeResults(); else { q.value = ''; q.blur(); canvas.focus(); } e.preventDefault(); }
  });
  results.addEventListener('pointerdown', e => e.preventDefault());
  results.addEventListener('click', e => { const b = e.target.closest('[data-n]'); if (b) choose(+b.dataset.n); });
  results.addEventListener('pointerover', e => { const b = e.target.closest('[data-n]'); if (b) setActive(+b.dataset.n); });
  q.addEventListener('blur', () => setTimeout(() => { if (document.activeElement !== q) closeResults(); }, 120));

  // ---- The trail of picks -------------------------------------------------

  const trailEl = document.getElementById('trail');
  function renderTrail() {
    trailEl.innerHTML = trail.slice(-6).map(id => {
      const n = byId.get(id);
      return `<button class="crumb" data-pick="${esc(id)}" ${id === current ? 'aria-current="true"' : ''} title="${esc(n.kind === 'part' ? n.text.name + ' › ' + n.name : n.name)}">${esc(label(n))}</button>`;
    }).join('');
  }
  trailEl.addEventListener('click', e => { const b = e.target.closest('[data-pick]'); if (b) pick(b.dataset.pick, { fly: true }); });

  // ---- The map's hands: pointer, wheel, pinch ------------------------------

  const pointers = new Map();
  let press = null; // the press under way: where and when it began, and its moves
  let lastTap = { t: 0, x: 0, y: 0 };
  let pinch = null;
  const local = e => { const r = canvas.getBoundingClientRect(); return [e.clientX - r.left, e.clientY - r.top]; };
  canvas.addEventListener('pointerdown', e => {
    stop();
    canvas.focus({ preventScroll: true });
    const [x, y] = local(e);
    pointers.set(e.pointerId, { x, y });
    canvas.setPointerCapture(e.pointerId);
    if (pointers.size === 2) {
      const [a, b] = [...pointers.values()];
      pinch = { d: Math.hypot(a.x - b.x, a.y - b.y), k: cam.k, mx: (a.x + b.x) / 2, my: (a.y + b.y) / 2 };
      press = null;
      return;
    }
    press = { x, y, t: performance.now(), moved: false, touch: e.pointerType === 'touch', samples: [[performance.now(), x, y]] };
  });
  canvas.addEventListener('pointermove', e => {
    const [x, y] = local(e);
    if (pointers.has(e.pointerId)) pointers.set(e.pointerId, { x, y });
    if (pinch && pointers.size === 2) {
      const [a, b] = [...pointers.values()];
      const d = Math.hypot(a.x - b.x, a.y - b.y), mx = (a.x + b.x) / 2, my = (a.y + b.y) / 2;
      panBy(mx - pinch.mx, my - pinch.my);
      zoomAt(mx, my, pinch.k * d / pinch.d);
      pinch.mx = mx; pinch.my = my;
      return;
    }
    if (press) {
      const far = Math.hypot(x - press.x, y - press.y) > (press.touch ? 9 : 4);
      if (!press.moved && far) { press.moved = true; canvas.classList.add('dragging'); }
      if (press.moved) {
        const [, lx, ly] = press.samples[press.samples.length - 1];
        panBy(x - lx, y - ly);
        const now = performance.now();
        press.samples.push([now, x, y]);
        while (press.samples.length > 2 && now - press.samples[0][0] > 90) press.samples.shift();
      }
      return;
    }
    const n = nearest(x, y);
    const id = n ? n.id : null;
    canvas.classList.toggle('over', !!n);
    if (id !== hover) { hover = id; light = id && id !== current ? lights(id) : lit; draw(); }
  });
  const release = e => {
    pointers.delete(e.pointerId);
    canvas.classList.remove('dragging');
    if (pinch) { if (pointers.size < 2) pinch = null; press = null; return; }
    const p = press;
    press = null;
    if (!p) return;
    const [x, y] = local(e);
    const now = performance.now();
    if (p.moved) {
      const s = p.samples;
      const [t0, x0, y0] = s[0], [t1, x1, y1] = s[s.length - 1];
      if (now - t1 < 60 && t1 > t0) glide((x1 - x0) / (t1 - t0), (y1 - y0) / (t1 - t0));
      return;
    }
    if (now - lastTap.t < 360 && Math.hypot(x - lastTap.x, y - lastTap.y) < 12) {
      zoomAt(x, y, cam.k * 2, true);
      lastTap = { t: 0, x: 0, y: 0 };
      return;
    }
    lastTap = { t: now, x, y };
    const n = nearest(x, y, p.touch ? 22 : 14);
    pick(n ? (n.id === current ? null : n.id) : null);
  };
  canvas.addEventListener('pointerup', release);
  canvas.addEventListener('pointercancel', e => { pointers.delete(e.pointerId); press = null; pinch = null; canvas.classList.remove('dragging'); });
  canvas.addEventListener('pointerleave', () => { if (!press && hover) { hover = null; light = lit; canvas.classList.remove('over'); draw(); } });

  let gesture = null; // Safari's and WebKit's own pinch, with its scale since it began
  canvas.addEventListener('wheel', e => {
    e.preventDefault();
    stop();
    if (gesture) return;
    const [x, y] = local(e);
    const unit = e.deltaMode === 1 ? 16 : e.deltaMode === 2 ? H : 1;
    let dx = e.deltaX * unit, dy = e.deltaY * unit;
    if (e.ctrlKey || e.metaKey) {
      const f = Math.exp(-dy / (e.ctrlKey ? 100 : 300));
      zoomAt(x, y, cam.k * Math.max(0.5, Math.min(2, f)));
      return;
    }
    if (e.shiftKey && !dx) { dx = dy; dy = 0; }
    panBy(-dx, -dy);
  }, { passive: false });
  canvas.addEventListener('gesturestart', e => { e.preventDefault(); stop(); gesture = { k: cam.k }; });
  canvas.addEventListener('gesturechange', e => {
    e.preventDefault();
    if (!gesture) return;
    const [x, y] = e.clientX !== undefined ? local(e) : [W / 2, H / 2];
    zoomAt(x, y, gesture.k * e.scale);
  });
  canvas.addEventListener('gestureend', e => { e.preventDefault(); gesture = null; });

  // ---- Keys -----------------------------------------------------------------

  // walk steps from the pick to the node lying most nearly that way among
  // those it is tied to, or else among all.
  function walk(dx, dy) {
    const n = current && byId.get(current);
    if (!n) { pick(byDate[0].id, { fly: true }); return; }
    const from = n.kind === 'part' ? n.text : n;
    const near = new Set([...lit.texts, ...lit.ideas]);
    near.delete(from.id);
    const score = m => {
      const vx = m.x - from.x, vy = m.y - from.y, d = Math.hypot(vx, vy);
      if (d < 1) return Infinity;
      const cos = (vx * dx + vy * dy) / d;
      if (cos < 0.4) return Infinity;
      return d * (2 - cos);
    };
    const best = list => { let b = null, bs = Infinity; for (const m of list) { const s = score(m); if (s < bs) { bs = s; b = m; } } return b; };
    const m = best([...near].map(id => byId.get(id))) || best([...texts, ...ideas]);
    if (m) pick(m.id, { fly: true });
  }
  document.addEventListener('keydown', e => {
    if (e.target === q || e.target.closest?.('[role="tree"]')) return;
    const keysOpen = !document.getElementById('keys').hidden;
    if (e.key === 'Escape') {
      if (keysOpen) { showKeys(false); e.preventDefault(); return; }
      if (current) { pick(null); e.preventDefault(); }
      return;
    }
    if (keysOpen) return;
    if ((e.metaKey || e.ctrlKey) && e.key === 'f') { q.focus(); q.select(); e.preventDefault(); return; }
    // ⌘← and ⌘→ go back and forward, and ⌘[ and ⌘] too where the host's
    // own menus don't take them.
    if (e.metaKey && (e.key === '[' || e.key === 'ArrowLeft')) { history.back(); e.preventDefault(); return; }
    if (e.metaKey && (e.key === ']' || e.key === 'ArrowRight')) { history.forward(); e.preventDefault(); return; }
    if (e.metaKey || e.ctrlKey || e.altKey) return;
    const onMap = e.target === canvas || e.target === document.body;
    const arrows = { ArrowLeft: [-1, 0], ArrowRight: [1, 0], ArrowUp: [0, -1], ArrowDown: [0, 1] };
    if (arrows[e.key] && onMap) {
      const [dx, dy] = arrows[e.key];
      if (e.shiftKey || !current) panBy(-dx * 90, -dy * 90); else walk(dx, dy);
      e.preventDefault();
      return;
    }
    switch (e.key) {
      case '/': q.focus(); q.select(); break;
      case '?': showKeys(true); break;
      case '+': case '=': zoomAt(W / 2, H / 2, cam.k * 1.5, true); break;
      case '-': case '_': zoomAt(W / 2, H / 2, cam.k / 1.5, true); break;
      case '0': go(fitView()); break;
      case 'Enter': if (onMap && current) { const n = byId.get(current); const m = n.kind === 'part' ? n.text : n; go({ x: m.x, y: m.y, k: Math.max(cam.k, fitK * 2.2) }); } else return; break;
      default: return;
    }
    e.preventDefault();
  });
  const keys = document.getElementById('keys');
  function showKeys(on) { keys.hidden = !on; if (on) keys.querySelector('div').focus?.(); else canvas.focus(); }
  document.getElementById('keys-open').addEventListener('click', () => showKeys(true));
  keys.addEventListener('click', e => { if (e.target === keys) showKeys(false); });
  document.getElementById('zoom-in').addEventListener('click', () => zoomAt(W / 2, H / 2, cam.k * 1.5, true));
  document.getElementById('zoom-out').addEventListener('click', () => zoomAt(W / 2, H / 2, cam.k / 1.5, true));
  document.getElementById('zoom-fit').addEventListener('click', () => go(fitView()));

  // ---- Size, theme, history and the view kept across reloads ----------------

  const KEEP = 'charters-graph/1';
  let saveTimer = 0;
  function save() {
    clearTimeout(saveTimer);
    saveTimer = setTimeout(saveNow, 250);
  }
  function saveNow() {
    try { sessionStorage.setItem(KEEP, JSON.stringify({ cam: { x: cam.x, y: cam.y, k: cam.k }, open: [...open], W, H })); } catch (_) { /* private browsing keeps nothing */ }
  }
  window.addEventListener('pagehide', saveNow);
  function resize() {
    const r = canvas.getBoundingClientRect();
    dpr = window.devicePixelRatio || 1;
    const was = W;
    W = Math.max(1, r.width); H = Math.max(1, r.height);
    canvas.width = Math.round(W * dpr);
    canvas.height = Math.round(H * dpr);
    fitK = fitView().k;
    if (!was) return;
    drawNow();
  }
  new ResizeObserver(() => resize()).observe(canvas);
  window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', () => { readColours(); drawNow(); });
  window.addEventListener('popstate', () => pick(fromHash(), { push: false, fly: true }));
  // A fragment the host sets, as a link to a text would, picks it too.
  window.addEventListener('hashchange', () => { if (fromHash() !== current) pick(fromHash(), { push: false, fly: true }); });

  // The legend: what each kind of line means.
  document.getElementById('legend').innerHTML = HOWS.map(h => `<span>${howSample(h)}${esc(HOW[h].say)}</span>`).join('')
    + '<span><svg viewBox="0 0 26 8" aria-hidden="true"><circle cx="13" cy="4" r="3.6" fill="var(--card)" stroke="var(--muted)"/></svg>a text</span>'
    + '<span><svg viewBox="0 0 26 8" aria-hidden="true"><path d="M13 0 17 4 13 8 9 4z" fill="var(--panel)" stroke="var(--muted)"/></svg>a right or idea</span>';
  document.getElementById('count').textContent = `${texts.length} texts · ${G.links.length} links · ${ideas.length} rights and ideas`;

  // Start: the view as it was left, or everything; the pick from the address.
  resize();
  let kept = null;
  try { kept = JSON.parse(sessionStorage.getItem(KEEP) || 'null'); } catch (_) { kept = null; }
  if (kept && kept.cam && Number.isFinite(kept.cam.k)) { Object.assign(cam, kept.cam); open = new Set(kept.open || []); }
  else Object.assign(cam, fitView());
  current = null;
  pick(fromHash(), { push: false, render: true, fly: !kept });
  try { history.replaceState({ id: current }, ''); } catch (_) { /* ok */ }
  drawNow();

  // For the page's own test, and for anyone reading it in a console.
  window.chartersGraph = { pick, drawNow, cam, nodes: byId, sx, sy, current: () => current, lit: () => lit, search, found: () => found, choose, fold, spans, fitView, go };
})();
