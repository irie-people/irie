// The Term view of a session: its terminal as it shows it now, drawn live
// from the core's screen frames (subscribe with screen: true), in the
// terminal's own colours and at its own width. It fits that width to the
// phone; a pinch or a double tap zooms, and the phone keeps the zoom. A
// machine whose Irie can't stream its screen yet is read once a second
// with the screen op instead, as plain text.

import { h } from './ui.js';
import { Stream, call } from './net.js';

const ZOOM = 'irie.termZoom';
const READ = 12; // px: the text size a double tap zooms to
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
    this.zoom = loadZoom(); // null: fit the width
    this.size = 0;
    this.fitSize = 0;
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
      if (!this.pinch) this.stuck = this.nearBottom();
    }, { passive: true });
    this.gestures();
    if ('ResizeObserver' in window) {
      this.ro = new ResizeObserver(() => this.layout());
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
    const w = this.cols + 'ch';
    if (this.grid.style.width !== w) this.grid.style.width = w;
    this.layout();
    const rows = this.grid.children;
    while (rows.length < this.rows) this.grid.append(h('div', { class: 'trow' }));
    while (rows.length > this.rows) this.grid.lastChild.remove();
    let st = BLANK;
    for (let y = 0; y < this.rows; y++) {
      const text = this.lines[y] || '';
      const cx = this.cursor && this.cursor.y === y ? this.cursor.x : -1;
      const c = this.cache[y];
      if (c && c.text === text && c.cx === cx && same(c.from, st)) {
        st = c.end;
        continue;
      }
      const r = drawLine(text, st, cx);
      rows[y].innerHTML = r.html;
      this.cache[y] = { text, from: st, cx, end: r.end };
      st = r.end;
    }
    if (this.stuck) this.toBottom();
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

  /** Sets the text size for the zoom: the width fitted, or the size chosen, never smaller. */
  layout() {
    // A pinch sets the size itself until it ends.
    if (!this.cols || this.el.hidden || this.pinch) return;
    const avail = this.scroller.clientWidth - 2 * PAD;
    if (avail <= 0) return;
    this.fitSize = avail / (this.cols * chRatio());
    const size = this.zoom == null ? this.fitSize : Math.min(MAX, Math.max(this.fitSize, this.zoom));
    this.setSize(size);
  }

  setSize(size) {
    if (Math.abs(size - this.size) < 0.01) return;
    this.size = size;
    this.grid.style.fontSize = size.toFixed(2) + 'px';
    if (this.stuck) this.toBottom();
  }

  nearBottom() {
    const s = this.scroller;
    return s.scrollHeight - s.scrollTop - s.clientHeight < 24;
  }

  toBottom() {
    this.scroller.scrollTop = this.scroller.scrollHeight;
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

  /** Fits the width, or zooms in to read around a point of the screen (client coordinates). */
  toggleZoom(x, y) {
    if (!this.cols) return;
    if (x == null) {
      // The cursor, or else the bottom left, where Claude Code's prompt is.
      const g = this.grid.getBoundingClientRect();
      const r = this.scroller.getBoundingClientRect();
      const c = this.cursor;
      x = c ? g.left + PAD + c.x * this.size * chRatio() : r.left;
      y = c ? g.top + PAD + (c.y + 0.5) * this.size * LINE : r.bottom - 1;
    }
    const target = this.zoom == null ? Math.min(MAX, Math.max(READ, this.fitSize * 1.6)) : this.fitSize;
    this.zoomAround(target, x, y);
    this.zoom = this.zoom == null ? target : null;
    saveZoom(this.zoom);
    this.stuck = this.nearBottom();
    this.on.zoomed();
  }

  /** Where a point of the screen (client coordinates) falls on the grid, in its text size. */
  pointAt(x, y) {
    const g = this.grid.getBoundingClientRect();
    return { x: (x - g.left - PAD) / this.size, y: (y - g.top - PAD) / this.size };
  }

  /** Zooms to size, keeping the grid's point at (client coordinates) x, y, or the point given there. */
  zoomAround(size, x, y, at = this.pointAt(x, y)) {
    const stuck = this.stuck;
    this.stuck = false;
    this.setSize(size);
    const g = this.grid.getBoundingClientRect();
    this.scroller.scrollLeft += g.left + PAD + at.x * size - x;
    this.scroller.scrollTop += g.top + PAD + at.y * size - y;
    this.stuck = stuck && this.nearBottom();
  }

  gestures() {
    const s = this.scroller;
    const dist = (a, b) => Math.hypot(a.clientX - b.clientX, a.clientY - b.clientY);
    const mid = (a, b) => [(a.clientX + b.clientX) / 2, (a.clientY + b.clientY) / 2];
    s.addEventListener('touchstart', (e) => {
      if (e.touches.length === 2 && this.cols) {
        const [a, b] = e.touches;
        const [x, y] = mid(a, b);
        this.pinch = { d: Math.max(dist(a, b), 1), size: this.size, at: this.pointAt(x, y) };
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
      const size = Math.min(MAX, Math.max(this.fitSize, p.size * dist(a, b) / p.d));
      // The point first pinched follows the fingers.
      const [x, y] = mid(a, b);
      this.zoomAround(size, x, y, p.at);
    }, { passive: false });
    const end = (e) => {
      if (this.pinch && e.touches.length < 2) {
        this.pinch = null;
        this.zoom = this.size - this.fitSize < 0.3 ? null : this.size;
        if (this.zoom == null) this.setSize(this.fitSize);
        saveZoom(this.zoom);
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

/** The line height of a row, in its text size (.tgrid's line-height). */
const LINE = 1.2;

/** The state SGR sets: colours as CSS, attributes as flags. States are never changed in place. */
const BLANK = Object.freeze({ fg: '', bg: '', b: false, d: false, i: false, u: false, s: false, inv: false, hid: false });
const KEYS = Object.keys(BLANK);

/**
 * Draws one line as capture-pane -e writes it, from the state the line
 * before left: its HTML, and the state it leaves for the next. The cell at
 * column cx, when cx ≥ 0, carries the cursor.
 */
export function drawLine(text, from, cx) {
  let st = from;
  let html = '';
  let run = '';
  let col = 0;
  const flush = () => {
    if (run) html += wrap(st, run);
    run = '';
  };
  for (let i = 0; i < text.length;) {
    const ch = text[i];
    if (ch === '\x1b') {
      const n = text[i + 1];
      if (n === '[') {
        let j = i + 2;
        while (j < text.length && (text.charCodeAt(j) < 0x40 || text.charCodeAt(j) > 0x7e)) j++;
        if (text[j] === 'm') {
          const next = sgr(st, text.slice(i + 2, j));
          if (next !== st) {
            flush();
            st = next;
          }
        }
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
    if (w > 0 && col === cx) {
      flush();
      html += wrap(st, cell(c, cp, w), true);
      col += w;
      continue;
    }
    run += cell(c, cp, w);
    col += w;
  }
  flush();
  if (cx >= col) html += ' '.repeat(cx - col) + wrap(BLANK, ' ', true);
  return { html, end: st };
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

function loadZoom() {
  try {
    const v = parseFloat(localStorage.getItem(ZOOM));
    return v > 0 ? v : null;
  } catch {
    return null;
  }
}

function saveZoom(v) {
  try {
    if (v == null) localStorage.removeItem(ZOOM);
    else localStorage.setItem(ZOOM, v.toFixed(1));
  } catch {
    // Private mode: the zoom lasts while the app is open.
  }
}
