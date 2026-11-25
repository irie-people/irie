// The Common Schools page (docs/proposals/common-schools.md): pies that
// open into smaller pies. A pick is a step of the page's address, so Back
// and Forward walk the picks: "#began/section-16/ohio" is a slice, a pie
// of its own when it has parts, and "#text/land-ordinance" a text's
// words. Irie's own, with no library; it loads nothing.
(() => {
  'use strict';

  const data = JSON.parse(document.getElementById('schools-data').textContent);
  const $ = id => document.getElementById(id);
  const SVG = 'http://www.w3.org/2000/svg';
  const reduce = matchMedia('(prefers-reduced-motion: reduce)');
  // A pie has at most MAX slices: past that, the largest MAX-1 and one for
  // the rest, which opens into them.
  const MAX = 6;
  const TAU = Math.PI * 2;
  const TOP = -Math.PI / 2;

  // ---- The slices' colours, worked out from the design library ----------

  (() => {
    const vars = list => list.map((c, i) => `--s${i}: ${c};`).join(' ');
    const st = document.createElement('style');
    st.textContent = `:root { ${vars(data.colours.light)} } @media (prefers-color-scheme: dark) { :root { ${vars(data.colours.dark)} } }`;
    document.head.appendChild(st);
  })();
  const fill = slot => (slot < 0 ? 'var(--rest)' : `var(--s${slot % data.colours.light.length})`);

  // ---- Numbers -----------------------------------------------------------

  const whole = new Intl.NumberFormat('en-US', { maximumFractionDigits: 0 });
  function amount(v, unit) {
    switch (unit) {
      case 'states': return v === 1 ? '1 state' : whole.format(v) + ' states';
      case 'acres': return whole.format(v) + ' acres';
      case 'dollars': {
        // Whole from a hundred, to a tenth from ten, to a hundredth below.
        const at = (x, word) => {
          const d = x >= 100 ? 0 : x >= 10 ? 1 : 2;
          return '$' + (+x.toFixed(d)).toString() + ' ' + word;
        };
        if (v >= 1e9) return at(v / 1e9, 'billion');
        if (v >= 1e6) return at(v / 1e6, 'million');
        return '$' + whole.format(v);
      }
      case 'percent': return (Math.round(v * 10) / 10) + '%';
      case undefined: case '': return whole.format(v);
      // One of a plural unit is singular: 1 case, 1 bus, 1 vote.
      default: return whole.format(v) + ' ' + (v === 1 ? ({ cases: 'case', buses: 'bus', votes: 'vote' }[unit] || unit) : unit);
    }
  }
  // measure says what a pie's slices are sized by.
  function measure(unit) {
    return { states: 'the number of states', acres: 'acres', dollars: 'dollars', percent: 'its share', cases: 'the number of decisions', buses: 'the number of buses', votes: 'votes' }[unit] || unit || 'its share';
  }
  function share(s) {
    if (s >= 0.995 && s < 1) return '>99%';
    if (s >= 0.1) return Math.round(s * 100) + '%';
    if (s >= 0.001) return (Math.round(s * 1000) / 10) + '%';
    return '<0.1%';
  }

  // ---- The pies: every node by id, each with the parent it shows in ------

  const nodes = new Map();
  const parentOf = new Map();
  const lensOf = new Map();

  // fold gives a node's slices as its pie shows them, at most MAX, the last
  // the rest when there are more, itself folded the same way.
  function fold(n, lens) {
    nodes.set(n.id, n);
    lensOf.set(n.id, lens);
    const parts = n.parts || [];
    let shown = parts;
    if (parts.length > MAX) {
      const rest = parts.slice(MAX - 1);
      const sum = rest.reduce((a, p) => a + p.value, 0);
      const r = {
        id: n.id + '/~rest', name: rest.length + ' more', value: sum, unit: n.unit, rest: true,
        figure: amount(sum, n.unit) + ' in ' + rest.length + ' slices',
        title: (n.title || n.name) + ': the ' + rest.length + ' smallest',
        about: 'The ' + rest.length + ' smallest slices of the pie before, which has room for only ' + MAX + ', as a pie of their own.',
        source: n.source, where: n.where, parts: rest,
      };
      shown = parts.slice(0, MAX - 1).concat([r]);
    }
    n.shown = shown;
    for (const p of shown) {
      parentOf.set(p.id, n);
      fold(p, lens);
    }
    // A pie the card opens besides its own steps up to the slice that
    // holds it.
    for (const a of n.also || []) {
      parentOf.set(a.id, n);
      fold(a, lens);
    }
  }
  for (const l of data.lenses) fold(l.root, l);

  const hasPie = n => n.shown && n.shown.length >= 2;

  // ---- Where we are --------------------------------------------------------

  let route = null;   // {kind: 'node', id} or {kind: 'text', id}
  let lastNode = data.lenses[0].root.id;
  let drawn = null;   // the pie on the stage: {node, slices, cx, cy, r}
  let raf = 0;

  function parse(hash) {
    const h = decodeURIComponent((hash || '').replace(/^#/, ''));
    if (h.startsWith('text/')) {
      const id = h.slice(5);
      if (data.texts[id]) return { kind: 'text', id };
    }
    if (nodes.has(h)) return { kind: 'node', id: h };
    return { kind: 'node', id: data.lenses[0].root.id };
  }
  const go = id => { location.hash = '#' + id; };

  // The pie a node shows in: its own when it has one, its parent's
  // otherwise, with it picked.
  function pieFor(n) {
    if (hasPie(n)) return { pie: n, pick: null };
    const p = parentOf.get(n.id);
    return p ? { pie: p, pick: n } : { pie: n, pick: null };
  }

  function trailOf(n) {
    const out = [];
    for (let x = n; x; x = parentOf.get(x.id)) out.unshift(x);
    return out;
  }

  // ---- The lens choice and the trail -----------------------------------------

  const lenses = $('lenses');
  data.lenses.forEach((l, i) => {
    const b = document.createElement('button');
    b.className = 'lens';
    b.id = 'lens-' + l.id;
    b.setAttribute('role', 'tab');
    b.textContent = l.name;
    b.title = l.name + ' (' + (i + 1) + ')';
    b.addEventListener('click', () => go(l.root.id));
    b.addEventListener('keydown', e => {
      if (e.key !== 'ArrowRight' && e.key !== 'ArrowLeft') return;
      const all = [...lenses.querySelectorAll('.lens')];
      const k = (all.indexOf(b) + (e.key === 'ArrowRight' ? 1 : -1) + all.length) % all.length;
      all[k].focus();
      e.preventDefault();
    });
    lenses.appendChild(b);
  });

  function drawLenses(lens) {
    for (const b of lenses.querySelectorAll('.lens')) {
      const on = b.id === 'lens-' + lens.id;
      b.setAttribute('aria-selected', on ? 'true' : 'false');
      b.tabIndex = on ? 0 : -1;
    }
  }

  function drawTrail(n, extra) {
    const ol = document.createElement('ol');
    const steps = trailOf(n);
    steps.forEach((x, i) => {
      const li = document.createElement('li');
      const last = i === steps.length - 1 && !extra;
      const parent = parentOf.get(x.id);
      const k = parent ? parent.shown.indexOf(x) : -1;
      if (parent && k >= 0) {
        const dot = document.createElement('span');
        dot.className = 'dot';
        dot.style.background = fill(x.rest ? -1 : k);
        dot.setAttribute('aria-hidden', 'true');
        li.appendChild(dot);
      }
      if (last) {
        const s = document.createElement('span');
        s.textContent = x.name;
        s.setAttribute('aria-current', 'page');
        li.appendChild(s);
      } else {
        const a = document.createElement('a');
        a.href = '#' + x.id;
        a.textContent = x.name;
        li.appendChild(a);
      }
      ol.appendChild(li);
    });
    if (extra) {
      const li = document.createElement('li');
      const s = document.createElement('span');
      s.textContent = extra;
      s.setAttribute('aria-current', 'page');
      li.appendChild(s);
      ol.appendChild(li);
    }
    $('trail').replaceChildren(ol);
  }

  // ---- The pie -------------------------------------------------------------------

  const svg = $('pie');
  const chart = $('chart');
  const tip = $('tip');

  function layout(n) {
    const total = n.shown.reduce((a, p) => a + p.value, 0);
    let a = TOP;
    let slot = 0;
    return n.shown.map(p => {
      const span = total > 0 ? (p.value / total) * TAU : 0;
      const s = { node: p, a0: a, a1: a + span, share: total > 0 ? p.value / total : 0, slot: p.rest ? -1 : slot++ };
      a += span;
      return s;
    });
  }

  function geometry(labels) {
    const w = chart.clientWidth, h = chart.clientHeight;
    const room = labels ? Math.min(200, w * 0.27) : 12;
    const r = Math.max(56, Math.min(h / 2 - 22, (w - 2 * room) / 2, 300));
    return { w, h, cx: w / 2, cy: h / 2, r };
  }

  function arc(cx, cy, r, a0, a1) {
    if (a1 - a0 >= TAU - 1e-6) {
      return `M${cx - r},${cy} a${r},${r} 0 1,1 ${2 * r},0 a${r},${r} 0 1,1 ${-2 * r},0 Z`;
    }
    if (a1 - a0 <= 1e-6) return '';
    const large = a1 - a0 > Math.PI ? 1 : 0;
    const x0 = cx + r * Math.cos(a0), y0 = cy + r * Math.sin(a0);
    const x1 = cx + r * Math.cos(a1), y1 = cy + r * Math.sin(a1);
    return `M${cx},${cy} L${x0.toFixed(2)},${y0.toFixed(2)} A${r},${r} 0 ${large} 1 ${x1.toFixed(2)},${y1.toFixed(2)} Z`;
  }

  function el(name, attrs) {
    const e = document.createElementNS(SVG, name);
    for (const k in attrs) e.setAttribute(k, attrs[k]);
    return e;
  }

  // paint draws slices at the angles given, with nothing else: a frame of
  // an animation. under is a whole circle drawn first, in a slice's colour.
  function paint(g, slices, under) {
    const keep = svg.querySelector('title');
    svg.replaceChildren(keep);
    svg.setAttribute('viewBox', `0 0 ${g.w} ${g.h}`);
    if (under != null) svg.appendChild(el('path', { d: arc(g.cx, g.cy, g.r, 0, TAU), fill: fill(under), class: 'slice whole' }));
    for (const s of slices) {
      const d = arc(g.cx, g.cy, g.r, s.a0, s.a1);
      if (d) svg.appendChild(el('path', { d, fill: fill(s.slot), class: 'slice' }));
    }
  }

  function drawPie(n, pick) {
    cancelAnimationFrame(raf);
    const slices = layout(n);
    const narrow = chart.clientWidth < 520;
    const g = geometry(!narrow);
    drawn = { node: n, slices, g, pick };
    const keep = svg.querySelector('title');
    svg.replaceChildren(keep);
    svg.setAttribute('viewBox', `0 0 ${g.w} ${g.h}`);
    $('pie-label').textContent = (n.title || n.name) + ': ' + slices.map(s => s.node.name + ' ' + share(s.share)).join(', ');
    slices.forEach((s, i) => {
      const d = arc(g.cx, g.cy, g.r, s.a0, s.a1);
      if (!d) return;
      const p = el('path', { d, fill: fill(s.slot), class: 'slice', 'data-i': i });
      if (pick && s.node === pick) p.classList.add('lit');
      p.addEventListener('pointerenter', e => hover(i, e));
      p.addEventListener('pointermove', e => moveTip(e));
      p.addEventListener('pointerleave', () => hover(-1));
      p.addEventListener('click', () => open(s.node));
      svg.appendChild(p);
    });
    if (!narrow) drawLabels(slices, g);
    if (pick) dimExcept(slices.findIndex(s => s.node === pick));
  }

  // drawLabels names each slice big enough to name on the pie's edge, a
  // short line out to it, the names on each side kept from overlapping.
  function drawLabels(slices, g) {
    const sides = { left: [], right: [] };
    slices.forEach((s, i) => {
      if (s.share < 0.025) return;
      const m = (s.a0 + s.a1) / 2;
      const side = Math.cos(m) >= 0 ? 'right' : 'left';
      sides[side].push({ s, i, m, y: g.cy + (g.r + 16) * Math.sin(m) });
    });
    const gap = 17;
    for (const side of ['left', 'right']) {
      const ls = sides[side].sort((a, b) => a.y - b.y);
      for (let k = 1; k < ls.length; k++) ls[k].y = Math.max(ls[k].y, ls[k - 1].y + gap);
      const bottom = g.h - 8;
      if (ls.length && ls[ls.length - 1].y > bottom) {
        ls[ls.length - 1].y = bottom;
        for (let k = ls.length - 2; k >= 0; k--) ls[k].y = Math.min(ls[k].y, ls[k + 1].y - gap);
      }
      for (const l of ls) {
        const dir = side === 'right' ? 1 : -1;
        const x0 = g.cx + (g.r + 2) * Math.cos(l.m), y0 = g.cy + (g.r + 2) * Math.sin(l.m);
        const x1 = g.cx + (g.r + 12) * Math.cos(l.m), y1 = g.cy + (g.r + 12) * Math.sin(l.m);
        const x2 = g.cx + dir * (g.r + 24);
        svg.appendChild(el('polyline', { points: `${x0},${y0} ${x1},${y1} ${x1 + (x2 - x1) * 0.15},${l.y} ${x2},${l.y}`, class: 'lead', 'data-i': l.i }));
        const t = el('text', { x: x2 + dir * 5, y: l.y + 4, 'text-anchor': side === 'right' ? 'start' : 'end', class: 'label', 'data-i': l.i });
        const name = document.createTextNode(l.s.node.name + ' ');
        t.appendChild(name);
        const sh = el('tspan', { class: 'share' });
        sh.textContent = share(l.s.share);
        t.appendChild(sh);
        svg.appendChild(t);
        // A name too long for the room beside the pie is cut short; the
        // table gives it whole.
        const room = side === 'right' ? g.w - (x2 + 5) - 4 : x2 - 5 - 4;
        let full = l.s.node.name, cut = full.length;
        while (cut > 1 && t.getComputedTextLength() > room) {
          cut--;
          name.textContent = full.slice(0, cut).trimEnd() + '… ';
        }
        if (t.getComputedTextLength() > room) t.remove();
      }
    }
  }

  function dimExcept(i) {
    for (const p of svg.querySelectorAll('.slice, .label, .lead')) {
      const k = +p.getAttribute('data-i');
      p.classList.toggle('dim', i >= 0 && k !== i);
    }
  }

  function hover(i, e) {
    const pick = drawn && drawn.pick ? drawn.slices.findIndex(s => s.node === drawn.pick) : -1;
    dimExcept(i >= 0 ? i : pick);
    for (const tr of document.querySelectorAll('.parts tbody tr')) tr.classList.toggle('on', +tr.dataset.i === i);
    if (i < 0 || !drawn) { tip.hidden = true; return; }
    const s = drawn.slices[i];
    tip.replaceChildren();
    const b = document.createElement('b');
    b.textContent = s.node.name;
    const sp = document.createElement('span');
    sp.textContent = (s.node.figure || amount(s.node.value, drawn.node.unit)) + ' · ' + share(s.share);
    tip.append(b, sp);
    tip.hidden = false;
    if (e) moveTip(e);
  }

  function moveTip(e) {
    const r = chart.getBoundingClientRect();
    const x = Math.min(e.clientX - r.left + 14, r.width - tip.offsetWidth - 4);
    const y = Math.min(e.clientY - r.top + 14, r.height - tip.offsetHeight - 4);
    tip.style.left = Math.max(4, x) + 'px';
    tip.style.top = Math.max(4, y) + 'px';
  }

  // ---- Opening and closing a slice, as motion --------------------------------

  // animate steps an animation of ms milliseconds on the screen's frames,
  // then calls done. A frame that never comes, as in a window out of
  // sight, can't hold it: a timer ends it. A newer step of the page ends
  // it where it stands, and done is never called.
  let gen = 0, late = 0;
  function animate(ms, step, done) {
    cancelAnimationFrame(raf);
    clearTimeout(late);
    const my = gen;
    if (reduce.matches || document.hidden) { done(); return; }
    let over = false;
    const end = () => {
      if (over || my !== gen) return;
      over = true;
      cancelAnimationFrame(raf);
      clearTimeout(late);
      step(1);
      done();
    };
    const t0 = performance.now();
    const ease = t => 1 - Math.pow(1 - t, 3);
    const frame = now => {
      if (over || my !== gen) return;
      const t = Math.min(1, (now - t0) / ms);
      step(ease(t));
      if (t < 1) raf = requestAnimationFrame(frame);
      else end();
    };
    raf = requestAnimationFrame(frame);
    late = setTimeout(end, ms + 250);
  }

  // expand runs the slice i of the pie drawn out to the whole circle, the
  // others closing up beside it (or, backward, the whole circle back to
  // the slice).
  function expand(from, i, backward, done) {
    const g = geometry(false);
    g.r = from.g.r;
    const c = from.slices[i];
    const m = (c.a0 + c.a1) / 2;
    const target = from.slices.map((s, k) => (k < i ? [m - Math.PI, m - Math.PI] : k > i ? [m + Math.PI, m + Math.PI] : [m - Math.PI, m + Math.PI]));
    animate(320, t => {
      const u = backward ? 1 - t : t;
      paint(g, from.slices.map((s, k) => ({ slot: s.slot, a0: s.a0 + (target[k][0] - s.a0) * u, a1: s.a1 + (target[k][1] - s.a1) * u })));
    }, done);
  }

  // sweep draws the pie n's slices sweeping round from the top over a
  // whole circle in the colour of the slice it opened from (or back).
  function sweep(n, under, backward, done) {
    const slices = layout(n);
    const g = geometry(chart.clientWidth >= 520);
    animate(360, t => {
      const u = backward ? 1 - t : t;
      paint(g, slices.map(s => ({ slot: s.slot, a0: TOP + (s.a0 - TOP) * u, a1: TOP + (s.a1 - TOP) * u })), under);
    }, done);
  }

  // ---- The card ----------------------------------------------------------------

  const card = $('card');

  function h(tag, cls, text) {
    const e = document.createElement(tag);
    if (cls) e.className = cls;
    if (text != null) e.textContent = text;
    return e;
  }

  // sourceLine names where a slice's figures come from, and the other
  // sources its words draw on, each with a link.
  function sourceLine(n) {
    const ids = (n.source ? [n.source] : []).concat(n.more || []);
    const srcs = ids.map(id => data.sources[id]).filter(Boolean);
    if (!srcs.length) return null;
    const p = h('p', 'source');
    srcs.forEach((src, i) => {
      p.append((i === 0 ? 'Source: ' : ' Also: ') + src.title + ', ' + src.by + (i === 0 && n.where ? ', ' + n.where : '') + '. ');
      const a = h('a', null, 'Read it');
      a.href = src.url;
      a.rel = 'noopener';
      a.target = '_blank';
      p.append(a, '.');
    });
    return p;
  }

  function textsBlock(ids, heading) {
    if (!ids || !ids.length) return [];
    const ul = h('ul', 'texts');
    for (const id of ids) {
      const t = data.texts[id];
      if (!t) continue;
      const b = document.createElement('button');
      b.append(h('b', null, t.title), h('span', null, t.place + ', ' + year(t.when) + ' · ' + t.kind));
      b.addEventListener('click', () => go('text/' + id));
      const li = h('li');
      li.appendChild(b);
      ul.appendChild(li);
    }
    return [h('h3', null, heading), ul];
  }

  const year = when => (when || '').slice(0, 4);

  // nodeBlock is what the card says of a slice: its name and amount, the
  // table of its own slices when it is the pie shown, what it is, and the
  // texts and the source behind it.
  function nodeBlock(n, pie) {
    const out = [h('h2', null, n.name)];
    const fig = pie ? n.whole || n.figure : n.figure;
    if (fig) out.push(h('p', 'figure', fig));
    if (pie) out.push(table(n, null));
    if (n.about) out.push(h('p', 'about', n.about));
    if (n.note) out.push(h('p', 'note', n.note));
    if (n.list && n.list.length) {
      const ul = h('ul', 'list');
      for (const x of n.list) ul.appendChild(h('li', null, x));
      out.push(ul);
    }
    if (n.also && n.also.length) {
      out.push(h('h3', null, 'Another way to count it'));
      const ul = h('ul', 'texts');
      for (const a of n.also) {
        const b = document.createElement('button');
        b.append(h('b', null, a.title || a.name), h('span', null, a.whole || a.figure));
        b.addEventListener('click', () => go(a.id));
        const li = h('li');
        li.appendChild(b);
        ul.appendChild(li);
      }
      out.push(ul);
    }
    out.push(...textsBlock(n.texts, 'The texts behind it'));
    const s = sourceLine(n);
    if (s) out.push(s);
    return out;
  }

  function table(n, pick) {
    const slices = drawn && drawn.node === n ? drawn.slices : layout(n);
    const t = h('table', 'parts');
    const cap = h('caption', null, (n.title || n.name) + ', each slice sized by ' + measure(n.unit));
    t.appendChild(cap);
    const head = h('thead');
    const hr = h('tr');
    hr.append(h('th', null, 'Slice'), h('th', 'num', 'Amount'), h('th', 'num', 'Share'));
    head.appendChild(hr);
    t.appendChild(head);
    const body = h('tbody');
    slices.forEach((s, i) => {
      const tr = h('tr');
      tr.dataset.i = i;
      if (pick && s.node === pick) tr.classList.add('picked');
      const td = h('td');
      const b = h('button', 'row');
      const sw = h('span', 'swatch');
      sw.style.background = fill(s.slot);
      sw.setAttribute('aria-hidden', 'true');
      b.append(sw, h('span', 'label-text', s.node.name));
      if (hasPie(s.node)) {
        const o = h('span', 'opens', '›');
        o.setAttribute('aria-hidden', 'true');
        b.appendChild(o);
        b.setAttribute('aria-label', s.node.name + ', opens into its parts');
      }
      b.addEventListener('click', () => open(s.node));
      b.addEventListener('focus', () => hover(i));
      b.addEventListener('blur', () => hover(-1));
      b.addEventListener('pointerenter', () => hover(i));
      b.addEventListener('pointerleave', () => hover(-1));
      b.addEventListener('keydown', e => {
        if (e.key !== 'ArrowDown' && e.key !== 'ArrowUp') return;
        const rows = [...body.querySelectorAll('.row')];
        const k = rows.indexOf(b) + (e.key === 'ArrowDown' ? 1 : -1);
        if (rows[k]) rows[k].focus();
        e.preventDefault();
      });
      td.appendChild(b);
      tr.append(td, h('td', 'num', amount(s.node.value, n.unit)), h('td', 'num', share(s.share)));
      body.appendChild(tr);
    });
    t.appendChild(body);
    return t;
  }

  function drawCard(pie, pick) {
    const out = [];
    if (pick) {
      out.push(...nodeBlock(pick, false));
      out.push(h('h3', null, 'In the pie: ' + pie.name), table(pie, pick));
    } else {
      out.push(...nodeBlock(pie, true));
    }
    card.replaceChildren(...out);
    card.scrollTop = 0;
  }

  // ---- A text's words --------------------------------------------------------------

  const reader = $('reader');

  function esc(s) {
    return s.replace(/[&<>"]/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' }[c]));
  }

  // words writes a passage, its bold kept and its passages about schools
  // lit, each found word for word.
  function words(text, lit) {
    let s = esc(text);
    for (const l of lit || []) {
      const e = esc(l);
      const i = s.indexOf(e);
      if (i >= 0) s = s.slice(0, i) + '<mark>' + e + '</mark>' + s.slice(i + e.length);
    }
    return s.replace(/\*\*([^*]+)\*\*/g, '<b>$1</b>');
  }

  function drawText(id) {
    const t = data.texts[id];
    reader.replaceChildren();
    reader.appendChild(h('p', 'about', t.about));
    for (const b of t.blocks) {
      if (b.kind === 'h2' || b.kind === 'h3' || b.kind === 'h4') {
        const e = document.createElement(b.kind);
        e.innerHTML = words(b.text, null);
        reader.appendChild(e);
      } else if (b.kind === 'note') {
        reader.appendChild(h('p', 'note', b.text));
      } else if (b.kind === 'list') {
        const ul = h('ul');
        for (const x of b.items) ul.appendChild(h('li', null, x));
        reader.appendChild(ul);
      } else {
        const p = document.createElement('p');
        p.innerHTML = words(b.text, t.lit);
        reader.appendChild(p);
      }
    }
    $('title').textContent = t.title;
    $('sub').textContent = t.place + ', ' + niceDate(t.when) + ' · ' + t.kind;
    // The card: back to where you were, the links, the fingerprint.
    const out = [];
    const back = h('button', 'back', '← ' + (nodes.get(lastNode) || {}).name);
    back.addEventListener('click', () => go(lastNode));
    out.push(back, h('h2', null, t.short), h('p', 'figure', niceDate(t.when)));
    const from = data.links.filter(l => l.from === id), to = data.links.filter(l => l.to === id);
    if (from.length) out.push(h('h3', null, 'What it drew on'), linkList(from, 'to'));
    if (to.length) out.push(h('h3', null, 'What drew on it'), linkList(to, 'from'));
    const where = [];
    for (const l of data.lenses) walk(l.root, n => { if ((n.texts || []).includes(id)) where.push(n); });
    if (where.length) {
      out.push(h('h3', null, 'Where it shows'));
      const ul = h('ul', 'links');
      for (const n of where) {
        const li = h('li');
        const b = h('button', null, trailOf(n).map(x => x.name).join(' › '));
        b.addEventListener('click', () => go(n.id));
        li.appendChild(b);
        ul.appendChild(li);
      }
      out.push(ul);
    }
    out.push(h('h3', null, 'Its fingerprint'), h('p', 'fingerprint', 'SHA-512 ' + t.sha512));
    out.push(h('p', 'source', 'From the ' + t.skill + ' skill’s ' + t.file + '.'));
    card.replaceChildren(...out);
    card.scrollTop = 0;
  }

  function walk(n, f) {
    f(n);
    for (const p of n.parts || []) walk(p, f);
    for (const p of n.also || []) walk(p, f);
  }

  function linkList(ls, end) {
    const ul = h('ul', 'links');
    for (const l of ls) {
      const other = data.texts[l[end]];
      if (!other) continue;
      const li = h('li');
      const b = h('button', null, other.short);
      b.addEventListener('click', () => go('text/' + other.id));
      li.append(h('span', 'how', end === 'to' ? l.how + ' ' : 'which ' + l.how + ' this: '), b, h('p', null, l.why));
      ul.appendChild(li);
    }
    return ul;
  }

  const months = ['January', 'February', 'March', 'April', 'May', 'June', 'July', 'August', 'September', 'October', 'November', 'December'];
  function niceDate(when) {
    const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(when || '');
    return m ? `${+m[3]} ${months[+m[2] - 1]} ${m[1]}` : when;
  }

  // ---- Drawing a step ------------------------------------------------------------

  function show(next, prev) {
    gen++;
    cancelAnimationFrame(raf);
    clearTimeout(late);
    tip.hidden = true;
    if (next.kind === 'text') {
      chart.hidden = true;
      reader.hidden = false;
      const lens = lensOf.get(lastNode) || data.lenses[0];
      drawLenses(lens);
      drawTrail(nodes.get(lastNode), data.texts[next.id].short);
      drawText(next.id);
      $('stage').scrollTop = 0;
      return;
    }
    reader.hidden = true;
    chart.hidden = false;
    const n = nodes.get(next.id);
    lastNode = n.id;
    const { pie, pick } = pieFor(n);
    drawLenses(lensOf.get(n.id));
    drawTrail(n);
    $('title').textContent = pie.title || pie.name;
    const total = pie.whole || pie.figure;
    $('sub').textContent = (total ? total + ' · ' : '') + 'each slice sized by ' + measure(pie.unit);
    const was = prev && prev.kind === 'node' ? nodes.get(prev.id) : null;
    const wasPie = was ? pieFor(was).pie : null;
    const finish = () => { drawPie(pie, pick); drawCard(pie, pick); };
    // Down: the pie on the stage opens one of its slices.
    if (drawn && wasPie === drawn.node && parentOf.get(pie.id) === wasPie && !pick) {
      const i = drawn.slices.findIndex(s => s.node === pie);
      if (i < 0) { finish(); return; }
      const under = drawn.slices[i].slot;
      drawCard(pie, null);
      expand(drawn, i, false, () => sweep(pie, under, false, finish));
      return;
    }
    // Up: the pie on the stage closes into its slice of the pie before.
    if (drawn && wasPie === drawn.node && parentOf.get(wasPie.id) === pie) {
      const before = { node: pie, slices: layout(pie), g: geometry(false) };
      before.g.r = geometry(chart.clientWidth >= 520).r;
      const i = before.slices.findIndex(s => s.node === wasPie);
      if (i < 0) { finish(); return; }
      const under = before.slices[i].slot;
      drawCard(pie, pick);
      sweep(wasPie, under, true, () => expand(before, i, true, () => {
        finish();
        const rows = card.querySelectorAll('.row');
        if (!pick && rows[i]) rows[i].focus({ preventScroll: true });
      }));
      return;
    }
    finish();
  }

  function open(n) { go(n.id); }

  function up() {
    if (!route) return;
    if (route.kind === 'text') { go(lastNode); return; }
    const n = nodes.get(route.id);
    const { pie, pick } = pieFor(n);
    if (pick) { go(pie.id); return; }
    const p = parentOf.get(n.id);
    if (p) go(p.id);
  }

  function onHash() {
    const next = parse(location.hash);
    const prev = route;
    route = next;
    show(next, prev);
  }

  // ---- Keys ------------------------------------------------------------------------

  const keys = $('keys');
  let keysFrom = null;
  function toggleKeys(on) {
    if (on) { keysFrom = document.activeElement; keys.hidden = false; keys.querySelector('h2').setAttribute('tabindex', '-1'); keys.querySelector('h2').focus(); }
    else { keys.hidden = true; if (keysFrom) keysFrom.focus(); }
  }
  $('keys-open').addEventListener('click', () => toggleKeys(true));
  keys.addEventListener('click', e => { if (e.target === keys) toggleKeys(false); });

  document.addEventListener('keydown', e => {
    if (!keys.hidden) {
      if (e.key === 'Escape' || e.key === '?') { toggleKeys(false); e.preventDefault(); }
      return;
    }
    const typing = e.target && (e.target.tagName === 'INPUT' || e.target.tagName === 'TEXTAREA');
    if (typing) return;
    if (e.key === '?') { toggleKeys(true); e.preventDefault(); return; }
    if (e.key === 'Escape') { up(); e.preventDefault(); return; }
    if ((e.metaKey || e.altKey) && e.key === 'ArrowLeft') { history.back(); e.preventDefault(); return; }
    if ((e.metaKey || e.altKey) && e.key === 'ArrowRight') { history.forward(); e.preventDefault(); return; }
    if ((e.metaKey && (e.key === '[' || e.key === ']'))) { e.key === '[' ? history.back() : history.forward(); e.preventDefault(); return; }
    if (!e.metaKey && !e.ctrlKey && !e.altKey && /^[1-9]$/.test(e.key)) {
      const l = data.lenses[+e.key - 1];
      if (l) { go(l.root.id); e.preventDefault(); }
    }
  });

  let resizing = 0;
  addEventListener('resize', () => {
    cancelAnimationFrame(resizing);
    resizing = requestAnimationFrame(() => { if (drawn && !chart.hidden) drawPie(drawn.node, drawn.pick); });
  });

  addEventListener('hashchange', onHash);
  if (!location.hash) history.replaceState(null, '', '#' + data.lenses[0].root.id);
  onHash();

  // For tests and the app: the page's state, read-only.
  window.commonSchools = {
    route: () => route && { kind: route.kind, id: route.id },
    pie: () => drawn && { id: drawn.node.id, slices: drawn.slices.map(s => ({ id: s.node.id, share: s.share })), pick: drawn.pick && drawn.pick.id },
    go,
  };
})();
