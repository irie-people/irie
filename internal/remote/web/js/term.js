// The Term view of a session: its terminal as it shows it now, drawn live
// from the core's screen frames (subscribe with screen: true), in the
// terminal's own colours. Where its width fits the phone at a size that
// reads, as on a phone on its side, it is drawn whole, cell for cell; where
// it doesn't, as upright, its lines wrap at the phone's edge at a size that
// reads. Fit draws it whole whatever the size; a pinch sets the size and a
// double tap goes between the two, and the phone keeps both. A machine
// whose Irie can't stream its screen yet is read once a second with the
// screen op instead, as plain text.

import { h } from './ui.js';
import { Stream, call } from './net.js';

const TEXT = 'irie.termText'; // the text size to read it at, as a pinch left it
const FIT = 'irie.termFit'; // '1': drawn whole; '0': read; none: as the width allows
const READ = 12; // px: the text size to read it at, until a pinch sets another
const WHOLE = 0.75; // a terminal whose width fits at this much of the size to read it at, or more, is drawn whole
const MAX = 30; // px: the largest
const PAD = 8; // px around the grid, as .tgrid's padding

export class TermView {
  /**
   * on.options(): the numbered choices of the prompt it waits on you with
   * (options, once read) changed; on.zoomed(): the zoom changed;
   * on.unpaired(msg).
   */
  constructor(conn, sid, on) {
    this.conn = conn;
    this.sid = sid;
    this.on = on;
    this.cols = 0;
    this.rows = 0;
    this.lines = [];
    this.cursor = null;
    this.options = [];
    this.cache = []; // per row: what it was drawn from, and the state it left
    this.textSize = loadText(); // px, to read it at
    this.fit = loadFit(); // true: drawn whole; false: read; null: as the width allows
    this.size = 0;
    this.fitSize = 0;
    this.wrapAt = 0; // the cells a line takes before it wraps; 0 while drawn whole
    this.moved = NaN; // where it last scrolled itself to
    this.stuck = true;
    this.state = 'connecting';
    this.problem = '';
    this.plain = false;
    this.running = false;
    this.pollGen = 0;
    this.el = this.build();
  }

  build() {
    this.grid = h('div', { class: 'tgrid', 'aria-label': 'Its terminal' });
    this.scroller = h('div', { class: 'tscroll' }, this.grid);
    this.noteEl = h('div', { class: 'tnote', role: 'status', hidden: true });
    this.scroller.addEventListener('scroll', () => {
      // A scroll event comes a frame late: one from its own move, by then
      // under a card that grew below, would read as the person scrolling
      // away from the bottom.
      if (Math.abs(this.scroller.scrollTop - this.moved) < 1) {
        this.moved = NaN;
        return;
      }
      if (!this.pinch) this.stuck = this.nearBottom();
    }, { passive: true });
    this.gestures();
    if ('ResizeObserver' in window) {
      // A new width can wrap its lines anew: drawn again.
      this.ro = new ResizeObserver(() => this.schedule());
      this.ro.observe(this.scroller);
    }
    return h('div', { class: 'termview', hidden: true }, this.scroller, this.noteEl);
  }

  // ---- Following the terminal -------------------------------------------------

  start() {
    if (this.running) return;
    this.running = true;
    this.renderNote();
    if (!this.conn.can('screen-live')) {
      this.startPolling();
      return;
    }
    this.stream = new Stream(this.conn.m, { session: this.sid, screen: true }, {
      frame: (f) => {
        if (f.t === 'screen' && f.screen) this.onScreen(f.screen);
      },
      state: (st) => {
        this.state = st;
        clearTimeout(this.fallback);
        // An Irie from before the live screen takes the subscription and
        // sends no screen: read it with the screen op instead.
        if (st === 'connected' && !this.cols) this.fallback = setTimeout(() => this.fallBack(), 6000);
        this.renderNote();
      },
      unpaired: (msg) => this.on.unpaired(msg),
      refused: (e) => {
        // An Irie that refuses the live screen still reads it.
        if (e.code === 'bad_args') {
          setTimeout(() => this.fallBack());
          return true;
        }
        this.problem = e.message;
        this.renderNote();
        return e.code === 'not_found';
      },
    });
    this.stream.start();
  }

  stop() {
    this.running = false;
    clearTimeout(this.fallback);
    if (this.stream) this.stream.stop();
    this.stream = null;
    this.pollGen++;
    clearTimeout(this.pollTimer);
  }

  destroy() {
    this.stop();
    if (this.ro) this.ro.disconnect();
  }

  /** Reconnects now, as when the app comes back to the front. */
  kick() {
    if (this.stream) this.stream.kick();
  }

  fallBack() {
    if (!this.running || this.cols) return;
    if (this.stream) this.stream.stop();
    this.stream = null;
    this.startPolling();
  }

  startPolling() {
    const gen = ++this.pollGen;
    const go = async () => {
      if (gen !== this.pollGen) return;
      if (document.visibilityState === 'visible') {
        try {
          const r = await call(this.conn.m, 'screen', { session: this.sid }, { timeout: 5000 });
          if (gen !== this.pollGen) return;
          const lines = r.lines || [];
          this.plain = true;
          this.state = 'connected';
          this.problem = '';
          this.onScreen({ reset: true, cols: Math.max(80, ...lines.map(width)), rows: Math.max(1, lines.length), lines, cursor: null, options: r.options || [] });
        } catch (e) {
          if (gen !== this.pollGen) return;
          if (e.status === 401) return this.on.unpaired(e.message);
          this.problem = e.message;
          this.renderNote();
        }
      }
      if (gen === this.pollGen) this.pollTimer = setTimeout(go, 1000);
    };
    go();
  }

  onScreen(sc) {
    if (sc.session && sc.session !== this.sid) return;
    if (sc.reset) {
      if (sc.cols !== this.cols || sc.rows !== this.rows) this.cache = [];
      this.cols = sc.cols;
      this.rows = sc.rows;
      this.lines = (sc.lines || []).slice(0, sc.rows);
      while (this.lines.length < this.rows) this.lines.push('');
    } else if (this.cols) {
      for (const l of sc.set || []) if (l.n >= 0 && l.n < this.rows) this.lines[l.n] = l.text;
    } else return; // what changed, before the whole: wait for a reset
    clearTimeout(this.fallback);
    this.problem = '';
    this.cursor = sc.cursor || null;
    const opts = sc.options || [];
    if (!this.read || JSON.stringify(opts) !== JSON.stringify(this.options)) {
      this.read = true;
      this.options = opts;
      this.on.options(opts);
    }
    this.schedule();
  }

  // ---- Drawing ------------------------------------------------------------------

  schedule() {
    if (this.scheduled) return;
    this.scheduled = true;
    requestAnimationFrame(() => {
      this.scheduled = false;
      this.render();
    });
  }

  render() {
    this.renderNote();
    if (!this.cols || this.el.hidden) return;
    if (!this.layout()) return;
    const wrapAt = this.wrapAt;
    const rows = this.grid.children;
    while (rows.length < this.rows) this.grid.append(h('div', { class: 'trow' }));
    while (rows.length > this.rows) this.grid.lastChild.remove();
    let st = BLANK;
    for (let y = 0; y < this.rows; y++) {
      const text = this.lines[y] || '';
      const cx = this.cursor && this.cursor.y === y ? this.cursor.x : -1;
      const c = this.cache[y];
      if (c && c.text === text && c.cx === cx && c.wrapAt === wrapAt && same(c.from, st)) {
        st = c.end;
        continue;
      }
      const r = drawLine(text, st, cx, wrapAt);
      rows[y].innerHTML = r.html;
      this.cache[y] = { text, from: st, cx, wrapAt, end: r.end };
      st = r.end;
    }
    // A pinch or a double tap holds the row it began on where it was; else the bottom stays in view.
    const hold = this.pinch || this.hold;
    this.hold = null;
    if (hold) this.keep(hold.at, hold.y);
    else if (this.stuck) this.toBottom();
  }

  renderNote() {
    let text = '';
    if (this.problem) text = this.problem;
    else if (!this.cols) text = this.state === 'offline' ? `Can’t reach ${this.conn.m.name} just now.` : 'Reading its terminal…';
    else if (this.state === 'offline' || this.state === 'connecting') text = `Reconnecting to ${this.conn.m.name}…`;
    else if (this.plain) text = `Plain text, once a second: ${this.conn.m.name}’s Irie can’t stream its terminal yet.`;
    if (this.noteEl.textContent !== text) this.noteEl.textContent = text;
    this.noteEl.hidden = !text;
    this.noteEl.classList.toggle('quiet', !!this.cols && !this.problem);
  }

  /**
   * Sets the text size, and where lines wrap: drawn whole, the width fitted;
   * else at the size to read it at, its lines wrapped at the width. Says
   * whether it has room to be drawn.
   */
  layout() {
    const avail = this.scroller.clientWidth - 2 * PAD;
    if (!this.cols || avail <= 0) return false;
    const ch = chRatio();
    this.fitSize = avail / (this.cols * ch);
    const whole = this.fit ?? this.wholeFits();
    let size = this.pinch ? this.pinch.size : whole ? this.fitSize : this.textSize;
    size = Math.min(Math.max(MAX, this.fitSize), Math.max(this.fitSize, size));
    const wrapAt = size > this.fitSize + 0.05 ? Math.max(16, Math.floor(avail / (size * ch))) : 0;
    if (Math.abs(size - this.size) >= 0.01) {
      this.size = size;
      this.grid.style.fontSize = size.toFixed(2) + 'px';
    }
    const was = this.wrapAt;
    this.wrapAt = wrapAt;
    const w = (wrapAt || this.cols) + 'ch';
    if (this.grid.style.width !== w) this.grid.style.width = w;
    this.grid.classList.toggle('wrap', wrapAt > 0);
    if ((was > 0) !== (wrapAt > 0)) this.on.zoomed();
    return true;
  }

  /** Whether it is drawn whole, cell for cell, rather than its lines wrapped to read. */
  whole() {
    return this.wrapAt === 0;
  }

  /** Whether the width, by itself, draws it whole: when the whole of it is drawn near the size to read it at. */
  wholeFits() {
    return this.fitSize >= this.textSize * WHOLE;
  }

  /** Keeps a choice of drawn whole or not; none while the width would choose the same by itself, as it then does on a phone turned. */
  choose(fit) {
    this.fit = fit === this.wholeFits() ? null : fit;
    saveZoom(this.fit, this.textSize);
  }

  nearBottom() {
    const s = this.scroller;
    return s.scrollHeight - s.scrollTop - s.clientHeight < 24;
  }

  toBottom() {
    this.scroller.scrollTop = this.scroller.scrollHeight;
    this.moved = this.scroller.scrollTop;
  }

  /** Shown or hidden with the Term view. */
  shown(on) {
    this.el.hidden = !on;
    if (on) {
      this.stuck = true;
      this.cache = [];
      this.size = 0;
      // What it asks may have changed while hidden: read it again.
      this.read = false;
      this.options = [];
      this.start();
      this.render();
    } else this.stop();
  }

  // ---- Zoom -----------------------------------------------------------------------

  /** Goes between drawn whole and wrapped to read, holding the row at a point of the screen (client y) where it was. */
  toggleZoom(x, y) {
    if (!this.cols) return;
    if (y == null) {
      // The cursor's row, or else the bottom, where Claude Code's prompt is.
      const row = this.cursor && this.grid.children[this.cursor.y];
      y = row ? row.getBoundingClientRect().top + 1 : this.scroller.getBoundingClientRect().bottom - 1;
    }
    const at = this.rowAt(y);
    const fit = !this.whole();
    // To read it, a size larger than drawn whole, as on a wide screen.
    if (!fit && this.textSize < this.fitSize * 1.25) this.textSize = Math.min(MAX, Math.max(READ, this.fitSize * 1.6));
    this.choose(fit);
    this.hold = { at, y };
    this.render();
    this.stuck = this.nearBottom();
    this.on.zoomed();
  }

  /** The row at a point of the screen (client y), and how far down it the point is. */
  rowAt(y) {
    const rows = this.grid.children;
    for (let i = 0; i < rows.length; i++) {
      const r = rows[i].getBoundingClientRect();
      if (y < r.bottom || i === rows.length - 1) return { i, frac: r.height > 0 ? Math.min(1, Math.max(0, (y - r.top) / r.height)) : 0 };
    }
    return null;
  }

  /** Scrolls the row's point back to y (client coordinates). */
  keep(at, y) {
    const row = at && this.grid.children[at.i];
    if (!row) return;
    const r = row.getBoundingClientRect();
    this.scroller.scrollTop += r.top + at.frac * r.height - y;
    this.moved = this.scroller.scrollTop;
  }

  gestures() {
    const s = this.scroller;
    const dist = (a, b) => Math.hypot(a.clientX - b.clientX, a.clientY - b.clientY);
    const midY = (a, b) => (a.clientY + b.clientY) / 2;
    s.addEventListener('touchstart', (e) => {
      if (e.touches.length === 2 && this.cols) {
        const [a, b] = e.touches;
        const y = midY(a, b);
        this.pinch = { d: Math.max(dist(a, b), 1), from: this.size, size: this.size, at: this.rowAt(y), y };
        this.down = null;
        this.lastTap = null;
      } else if (e.touches.length === 1) {
        const t = e.touches[0];
        this.down = { x: t.clientX, y: t.clientY, t: Date.now() };
      } else this.down = null;
    }, { passive: true });
    s.addEventListener('touchmove', (e) => {
      if (!this.pinch || e.touches.length !== 2) return;
      e.preventDefault();
      const [a, b] = e.touches;
      const p = this.pinch;
      // Its lines wrap anew at the size, and the row first pinched follows the fingers.
      p.size = Math.min(Math.max(MAX, this.fitSize), Math.max(this.fitSize, p.from * dist(a, b) / p.d));
      p.y = midY(a, b);
      this.schedule();
    }, { passive: false });
    const end = (e) => {
      if (this.pinch && e.touches.length < 2) {
        const p = this.pinch;
        this.pinch = null;
        // Pinched down to the width, it is drawn whole; else read at the size.
        const fit = p.size - this.fitSize < 0.3;
        if (!fit) this.textSize = p.size;
        this.choose(fit);
        this.hold = { at: p.at, y: p.y };
        this.render();
        this.stuck = this.nearBottom();
        this.on.zoomed();
        return;
      }
      // A tap is a touch that ends near where it began, soon; two of them close together zoom.
      const down = this.down;
      this.down = null;
      if (e.type !== 'touchend' || e.touches.length || e.changedTouches.length !== 1 || !down) return;
      const t = e.changedTouches[0];
      const now = Date.now();
      if (Math.hypot(t.clientX - down.x, t.clientY - down.y) > 12 || now - down.t > 400) {
        this.lastTap = null;
        return;
      }
      const last = this.lastTap;
      if (last && now - last.t < 350 && Math.hypot(t.clientX - last.x, t.clientY - last.y) < 30) {
        e.preventDefault();
        this.lastTap = null;
        this.toggleZoom(t.clientX, t.clientY);
      } else this.lastTap = { t: now, x: t.clientX, y: t.clientY };
    };
    s.addEventListener('touchend', end, { passive: false });
    s.addEventListener('touchcancel', end, { passive: true });
    // Safari's own pinch would zoom the whole page.
    for (const g of ['gesturestart', 'gesturechange']) s.addEventListener(g, (e) => e.preventDefault(), { passive: false });
    s.addEventListener('dblclick', (e) => {
      if (matchMedia('(pointer: fine)').matches) this.toggleZoom(e.clientX, e.clientY);
    });
  }
}

// ---- A line of the terminal ----------------------------------------------------------

/** The state SGR sets: colours as CSS, attributes as flags. States are never changed in place. */
const BLANK = Object.freeze({ fg: '', bg: '', b: false, d: false, i: false, u: false, s: false, inv: false, hid: false });
const KEYS = Object.keys(BLANK);

/**
 * Draws one line as capture-pane -e writes it, from the state the line
 * before left: its HTML, and the state it leaves for the next. The cell at
 * column cx, when cx ≥ 0, carries the cursor. With wrapAt, the line takes as
 * many lines of that many cells as it needs (wrapCells), one after another.
 */
export function drawLine(text, from, cx, wrapAt = 0) {
  const { cells, end } = parseLine(text, from);
  let col = 0;
  for (const c of cells) {
    if (c.w > 0 && col === cx) c.cur = true;
    col += c.w;
  }
  // The cursor past the end of what is written, on a blank cell.
  if (cx >= col) {
    for (; col < cx; col++) cells.push(space());
    cells.push({ ...space(), cur: true });
  }
  const lines = wrapAt > 0 ? wrapCells(cells, wrapAt) : [cells];
  return { html: lines.map(markup).join('\n'), end };
}

/** A line's cells: each character as HTML, the cells it takes and the state it is drawn in; and the state the line leaves. */
function parseLine(text, from) {
  let st = from;
  const cells = [];
  for (let i = 0; i < text.length;) {
    const ch = text[i];
    if (ch === '\x1b') {
      const n = text[i + 1];
      if (n === '[') {
        let j = i + 2;
        while (j < text.length && (text.charCodeAt(j) < 0x40 || text.charCodeAt(j) > 0x7e)) j++;
        if (text[j] === 'm') st = sgr(st, text.slice(i + 2, j));
        i = j + 1;
      } else if (n === ']') {
        // An operating system command, such as a link: to BEL or ESC \.
        let j = i + 2;
        while (j < text.length && text[j] !== '\x07' && text[j] !== '\x1b') j++;
        i = text[j] === '\x1b' ? j + 2 : j + 1;
      } else if (n === '(' || n === ')' || n === '*' || n === '+') i += 3;
      else i += 2;
      continue;
    }
    const cp = text.codePointAt(i);
    const c = cp > 0xffff ? text.slice(i, i + 2) : ch;
    i += c.length;
    if (cp < 0x20 || cp === 0x7f) continue;
    const w = cellWidth(cp);
    // A combining mark goes with the character before it.
    if (w === 0 && cells.length) cells[cells.length - 1].s += cell(c, cp, w);
    else cells.push({ s: cell(c, cp, w), w, st, cp });
  }
  return { cells, end: st };
}

/** Cells as HTML: each run in one state together, the cursor's cell on its own. */
function markup(cells) {
  let html = '';
  let run = '';
  let st = null;
  for (const c of cells) {
    if (c.cur) {
      if (run) html += wrap(st, run);
      run = '';
      st = null;
      html += wrap(c.st, c.s, true);
      continue;
    }
    if (c.st !== st) {
      if (run) html += wrap(st, run);
      run = '';
      st = c.st;
    }
    run += c.s;
  }
  if (run) html += wrap(st, run);
  return html;
}

const space = () => ({ s: ' ', w: 1, st: BLANK, cp: 0x20 });
const blank = (c) => c.cp === 0x20 && !c.cur;
/** Box drawing (U+2500–257F): a rule of it is cut at the width rather than wrapped. */
const boxLine = (cp) => cp >= 0x2500 && cp <= 0x257f;
/** A box's upright sides, which a wrapped line goes on beside. */
const SIDES = new Set([0x2502, 0x2503, 0x2506, 0x2507, 0x250a, 0x250b, 0x254e, 0x254f, 0x2551]);
/** What a box's right edge may be: a side, or a corner or tee that opens to the left. */
const EDGES = new Set([...SIDES, 0x2510, 0x2511, 0x2512, 0x2513, 0x2518, 0x2519, 0x251a, 0x251b, 0x2524, 0x2528, 0x252b, 0x2557, 0x255d, 0x2562, 0x2563, 0x256e, 0x256f]);

/**
 * A line's cells as the lines they take at a width of W cells. Its blanks
 * at the end go. Wider than W, a box's right edge goes with the blanks
 * before it, a rule of box lines is cut at W, and anything else wraps: at a
 * space where the word after it isn't long, else at W, and goes on under its
 * indent: its blanks at the start, and a mark that leads it (Claude Code's ⏺
 * or ⎿, a list's -, a box's side) with the blanks after it, the side drawn
 * again.
 */
export function wrapCells(cells, W) {
  let n = cells.length;
  while (n > 0 && blank(cells[n - 1])) n--;
  const widthOf = (line, a, b) => {
    let w = 0;
    for (let k = a; k < b; k++) w += line[k].w;
    return w;
  };
  if (widthOf(cells, 0, n) > W && n >= 3 && EDGES.has(cells[n - 1].cp) && !cells[n - 1].cur && blank(cells[n - 2]) && blank(cells[n - 3])) {
    n--;
    while (n > 0 && blank(cells[n - 1])) n--;
  }
  const line = cells.slice(0, n);
  if (widthOf(line, 0, n) <= W) return [line];
  if (line.every((c) => blank(c) || boxLine(c.cp))) {
    let w = 0;
    let k = 0;
    while (k < n && w + line[k].w <= W) w += line[k++].w;
    return [line.slice(0, k)];
  }
  // The indent its lines go on under.
  let i = 0;
  while (i < n && blank(line[i])) i++;
  const lead = widthOf(line, 0, i);
  let indent = lead;
  let side = null;
  const mark = line[i];
  // Claude Code keeps a mark with its text by a no-break space, as in "⎿  ".
  const gap = (c) => blank(c) || c.cp === 0xa0;
  if (mark && !mark.cur && !/[\p{L}\p{N}]/u.test(String.fromCodePoint(mark.cp)) && i + 1 < n && gap(line[i + 1])) {
    let j = i + 1;
    while (j < n && gap(line[j])) j++;
    indent = widthOf(line, 0, j);
    if (SIDES.has(mark.cp)) side = mark;
  }
  if (indent > W / 3) {
    indent = Math.floor(W / 3);
    if (lead >= indent) side = null;
  }
  const out = [];
  let start = 0;
  while (start < n) {
    const first = out.length === 0;
    const room = first ? W : W - indent;
    let k = start;
    let w = 0;
    while (k < n && w + line[k].w <= room) w += line[k++].w;
    let end = k;
    let next = k;
    if (k === start) end = next = k + 1; // a cell wider than the room, on a line of its own
    else if (k < n && !blank(line[k])) {
      // Back to the space before the word it would cut, unless that word is long.
      let p = k - 1;
      while (p > start && !blank(line[p])) p--;
      if (p > start && widthOf(line, p + 1, k) < room * 0.4) {
        end = p;
        next = p + 1;
      }
    }
    while (end > start && blank(line[end - 1])) end--;
    const part = line.slice(start, end);
    if (first) out.push(part);
    else {
      const head = [];
      for (let x = 0; x < indent; x++) head.push(side && x === lead ? side : space());
      out.push(head.concat(part));
    }
    start = next;
    while (start < n && blank(line[start])) start++;
  }
  return out;
}

/** A run of cells in the state's colours and attributes; the cursor's cell inverted. */
function wrap(st, inner, cursor = false) {
  let fg = st.fg;
  let bg = st.bg;
  if (st.inv !== cursor) {
    fg = st.bg || 'var(--term-bg)';
    bg = st.fg || 'var(--term-fg)';
  }
  const cls = [];
  if (st.b) cls.push('b');
  if (st.d) cls.push('d');
  if (st.i) cls.push('i');
  if (st.u) cls.push('u');
  if (st.s) cls.push('s');
  if (st.hid) cls.push('h');
  if (cursor) cls.push('tcur');
  let style = '';
  if (fg) style += 'color:' + fg + ';';
  if (bg) style += 'background:' + bg + ';';
  if (!style && !cls.length) return inner;
  return '<span' + (cls.length ? ' class="' + cls.join(' ') + '"' : '') + (style ? ' style="' + style + '"' : '') + '>' + inner + '</span>';
}

/**
 * A character as HTML. One that isn't ASCII sits in a box as wide as its
 * cells, so a glyph from another font, as a symbol often is, keeps the
 * columns after it in place.
 */
function cell(c, cp, w) {
  if (cp < 0x80) return c === '<' ? '&lt;' : c === '>' ? '&gt;' : c === '&' ? '&amp;' : c;
  if (w === 0) return c;
  return '<span class="c' + w + '">' + c + '</span>';
}

/** The state after an SGR sequence's parameters (ECMA-48 §8.3.117, with the 256 and direct colours of ITU T.416). */
export function sgr(from, params) {
  const st = { ...from };
  const ps = params === '' ? ['0'] : params.split(';');
  for (let k = 0; k < ps.length; k++) {
    const sub = ps[k].split(':').map((x) => parseInt(x, 10) || 0);
    const n = sub[0];
    if (n === 38 || n === 48 || n === 58) {
      let c = '';
      if (sub.length > 1) {
        // The colon form: 38:5:N, or 38:2:[space]:R:G:B.
        if (sub[1] === 5) c = colour(sub[2]);
        else if (sub[1] === 2) c = sub.length >= 6 ? rgb(sub[3], sub[4], sub[5]) : rgb(sub[2], sub[3], sub[4]);
      } else if (ps[k + 1] === '5') {
        c = colour(parseInt(ps[k + 2], 10) || 0);
        k += 2;
      } else if (ps[k + 1] === '2') {
        c = rgb(parseInt(ps[k + 2], 10), parseInt(ps[k + 3], 10), parseInt(ps[k + 4], 10));
        k += 4;
      }
      if (n === 38) st.fg = c;
      else if (n === 48) st.bg = c;
      continue;
    }
    if (n === 0) Object.assign(st, BLANK);
    else if (n === 1) st.b = true;
    else if (n === 2) st.d = true;
    else if (n === 3) st.i = true;
    else if (n === 4) st.u = sub.length < 2 || sub[1] !== 0;
    else if (n === 7) st.inv = true;
    else if (n === 8) st.hid = true;
    else if (n === 9) st.s = true;
    else if (n === 21) st.u = true;
    else if (n === 22) st.b = st.d = false;
    else if (n === 23) st.i = false;
    else if (n === 24) st.u = false;
    else if (n === 27) st.inv = false;
    else if (n === 28) st.hid = false;
    else if (n === 29) st.s = false;
    else if (n >= 30 && n <= 37) st.fg = colour(n - 30);
    else if (n === 39) st.fg = '';
    else if (n >= 40 && n <= 47) st.bg = colour(n - 40);
    else if (n === 49) st.bg = '';
    else if (n >= 90 && n <= 97) st.fg = colour(n - 90 + 8);
    else if (n >= 100 && n <= 107) st.bg = colour(n - 100 + 8);
  }
  return same(st, from) ? from : Object.freeze(st);
}

/** Whether two states draw alike. */
function same(a, b) {
  if (a === b) return true;
  for (const k of KEYS) if (a[k] !== b[k]) return false;
  return true;
}

/** One of the 256 colours: the 16 of the palette (app.css), xterm's 6×6×6 cube, then its 24 greys. */
export function colour(n) {
  n = Math.max(0, Math.min(255, n | 0));
  if (n < 16) return `var(--t${n})`;
  if (n < 232) {
    const lv = [0, 95, 135, 175, 215, 255];
    n -= 16;
    return `rgb(${lv[Math.floor(n / 36)]},${lv[Math.floor(n / 6) % 6]},${lv[n % 6]})`;
  }
  const g = 8 + 10 * (n - 232);
  return `rgb(${g},${g},${g})`;
}

function rgb(r, g, b) {
  const v = (x) => Math.max(0, Math.min(255, x | 0));
  return `rgb(${v(r)},${v(g)},${v(b)})`;
}

/** The cells a character takes: none for a combining mark, two for a wide one (UAX #11), else one. */
export function cellWidth(cp) {
  if (cp < 0x300) return 1;
  if ((cp <= 0x36f) || (cp >= 0x200b && cp <= 0x200f) || (cp >= 0x20d0 && cp <= 0x20ff) || (cp >= 0xfe00 && cp <= 0xfe0f) || (cp >= 0xe0100 && cp <= 0xe01ef)) return 0;
  if ((cp >= 0x1100 && cp <= 0x115f) || (cp >= 0x2e80 && cp <= 0xa4cf && cp !== 0x303f) || (cp >= 0xac00 && cp <= 0xd7a3) ||
    (cp >= 0xf900 && cp <= 0xfaff) || (cp >= 0xfe30 && cp <= 0xfe4f) || (cp >= 0xff00 && cp <= 0xff60) || (cp >= 0xffe0 && cp <= 0xffe6) ||
    (cp >= 0x1f300 && cp <= 0x1f64f) || (cp >= 0x1f900 && cp <= 0x1f9ff) || (cp >= 0x1fa70 && cp <= 0x1faff) || (cp >= 0x20000 && cp <= 0x3fffd)) return 2;
  return 1;
}

/** The cells a plain line takes. */
function width(line) {
  let n = 0;
  for (const c of line) n += cellWidth(c.codePointAt(0));
  return n;
}

// ---- Measuring and keeping the zoom ---------------------------------------------------

let ratio = 0;

/** The width of a cell of the terminal's font, for each pixel of its size. */
function chRatio() {
  if (ratio) return ratio;
  const probe = h('span', { class: 'tprobe', text: '0'.repeat(20) });
  document.body.append(probe);
  const w = probe.getBoundingClientRect().width;
  probe.remove();
  ratio = w > 0 ? w / 20 / 100 : 0.6;
  return ratio;
}

function loadText() {
  try {
    const v = parseFloat(localStorage.getItem(TEXT));
    return v > 0 ? v : READ;
  } catch {
    return READ;
  }
}

function loadFit() {
  try {
    const v = localStorage.getItem(FIT);
    return v === '1' ? true : v === '0' ? false : null;
  } catch {
    return null;
  }
}

function saveZoom(fit, size) {
  try {
    if (fit == null) localStorage.removeItem(FIT);
    else localStorage.setItem(FIT, fit ? '1' : '0');
    localStorage.setItem(TEXT, size.toFixed(1));
    // The zoom before lines wrapped, which a pinch set on a grid drawn whole.
    localStorage.removeItem('irie.termZoom');
  } catch {
    // Private mode: the zoom lasts while the app is open.
  }
}
