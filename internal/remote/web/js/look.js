// How a row looks: its state's name, glyph and colour role (DESIGN §5.3,
// mac/Sources/Irie/States.swift), the agents' own colours while they work
// (Theme.swift: agentSlot, agentHue, agentWashHex, agentHex, agentInkHex),
// and the short forms of ages, durations and token counts. The palette is
// Evergreen; app.css holds the same swatches as CSS variables.

export const PALETTE = {
  light: {
    sidebar: 0xF4F4EC, ground: 0xE9EBDF, card: 0xFBFBF6, panel: 0xF1F2E8, field: 0xFDFDF9,
    edge: 0xD8DCC9, hover: 0xF6F7EF, selection: 0xDDE5D3, needsTint: 0xFBE5DC,
    ink: 0x1E2B22, muted: 0x56635A,
    accent: 0x2F5D46, accentInk: 0x264D3A, accentFill: 0x9DB8A3, onAccent: 0x1E2B22,
    needs: 0xB42A1E, done: 0x3F6A5F, paused: 0x855700, frozen: 0x286873,
    sun: 0xD9A93F, waves: 0x5E8A6E,
  },
  dark: {
    sidebar: 0x141C17, ground: 0x0E1410, card: 0x1C2620, panel: 0x18221C, field: 0x202B24,
    edge: 0x34443A, hover: 0x25322A, selection: 0x2A3D31, needsTint: 0x4A251C,
    ink: 0xECF0E6, muted: 0xB3BFB5,
    accent: 0x9CC5A1, accentInk: 0xB3D6B8, accentFill: 0x9CC5A1, onAccent: 0x12201A,
    needs: 0xFF9585, done: 0x8FC3B2, paused: 0xEFC468, frozen: 0x7DC4CE,
    sun: 0xD9A93F, waves: 0x9CC5A1,
  },
};

// ---- States ---------------------------------------------------------------

/** The look of a row's state, as States.swift's Look. */
export function look(s) {
  switch (s && s.state) {
    case 'failed': return 'failed';
    case 'exited': return (s.exit_code ?? 0) === 0 ? 'exited' : 'exitedError';
    case 'frozen': return 'frozen';
    case 'paused': return 'paused';
    case 'needs_you': return 'needs';
    case 'stalled': return 'stalled';
    case 'pausing': return 'pausing';
    case 'starting': return 'starting';
    case 'done': return 'done';
    case 'idle': return 'idle';
    // The core takes a live session it has no events for to be working.
    default: return 'working';
  }
}

const LABELS = {
  failed: 'Failed', exited: 'Exited', exitedError: 'Exited with an error', frozen: 'Frozen',
  paused: 'Paused', needs: 'Needs you', stalled: 'Stalled', pausing: 'Pausing', working: 'Working',
  starting: 'Starting', done: 'To verify', idle: 'Idle',
};

/** A state in plain words; a parked finished turn is Parked, not To verify. */
export function label(s) {
  const lk = look(s);
  return lk === 'done' && s.parked ? 'Parked' : LABELS[lk];
}

/** Red: it needs you. */
export const loud = (lk) => lk === 'failed' || lk === 'needs' || lk === 'stalled';

/** Its name is drawn muted. */
export const quiet = (lk) => lk === 'exited' || lk === 'exitedError' || lk === 'starting' || lk === 'idle';

/** Pausing is dim yellow, an exit with an error code dim red. */
export const dimmed = (lk) => lk === 'pausing' || lk === 'exitedError';

/** The colour role of DESIGN §5.3, as a class suffix: r-needs, r-agent… */
export function role(lk, redOnDone) {
  switch (lk) {
    case 'failed': case 'needs': case 'stalled': case 'exitedError': return 'needs';
    case 'frozen': return 'frozen';
    case 'paused': case 'pausing': return 'paused';
    case 'working': return 'agent';
    case 'done': return redOnDone ? 'needs' : 'done';
    default: return 'muted';
  }
}

// The glyphs of DESIGN §5.3, drawn on StateMark's 16-point grid.
const X = '<path d="M4.5 4.5 11.5 11.5M11.5 4.5 4.5 11.5" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"/>';
const BARS = '<path d="M5.8 3.5v9M10.2 3.5v9" fill="none" stroke="currentColor" stroke-width="2.1" stroke-linecap="round"/>';
const GLYPHS = {
  failed: X,
  exitedError: X,
  exited: '<circle cx="8" cy="8" r="2.1" fill="currentColor"/>',
  frozen: '<path d="M8 2.6 13.4 8 8 13.4 2.6 8Z" fill="currentColor"/>',
  paused: BARS,
  pausing: BARS,
  needs: '<circle cx="8" cy="8" r="7" fill="currentColor" fill-opacity=".22"/><circle cx="8" cy="8" r="4.2" fill="currentColor"/>',
  stalled: '<path d="M8 3.2v6" fill="none" stroke="currentColor" stroke-width="2.1" stroke-linecap="round"/><circle cx="8" cy="12.6" r="1.25" fill="currentColor"/>',
  working: '<circle cx="8" cy="8" r="5.6" fill="none" stroke="currentColor" stroke-width="1.7"/><path d="M8 2.4A5.6 5.6 0 0 0 8 13.6Z" fill="currentColor"/>',
  starting: '<circle cx="8" cy="8" r="5.6" fill="none" stroke="currentColor" stroke-width="1.7" stroke-dasharray="1.7 2.3" stroke-linecap="round"/>',
  done: '<path d="M3.6 8.4 6.6 11.3 12.4 4.9" fill="none" stroke="currentColor" stroke-width="2.1" stroke-linecap="round" stroke-linejoin="round"/>',
  idle: '<circle cx="8" cy="8" r="4.6" fill="none" stroke="currentColor" stroke-width="1.7"/>',
};

/** A state's glyph as SVG markup, drawn in currentColor. */
export function glyph(lk) {
  return `<svg viewBox="0 0 16 16" aria-hidden="true" focusable="false">${GLYPHS[lk] || GLYPHS.working}</svg>`;
}

/** The classes of a row's mark: its colour role, its agent's slot, dimming. */
export function markClass(s, redOnDone) {
  const lk = look(s);
  let c = 'mark r-' + role(lk, redOnDone);
  if (lk === 'working') c += ' slot' + agentSlot(s.id);
  if (dimmed(lk)) c += ' dim';
  return c;
}

// ---- The agents' own colours ----------------------------------------------

const SLOTS = 6;

/** The slot for a session, the same one every time: FNV-1a of its id's UTF-8 bytes. */
export function agentSlot(id) {
  let h = 0x811c9dc5;
  for (const b of new TextEncoder().encode(String(id))) {
    h ^= b;
    h = Math.imul(h, 0x01000193) >>> 0;
  }
  return h % SLOTS;
}

const ch = (hex, shift) => (hex >> shift) & 0xff;

/** a moved toward b by t, channel by channel. */
export function mix(a, b, t) {
  const m = (s) => Math.round(ch(a, s) + (ch(b, s) - ch(a, s)) * t) << s;
  return (m(16) | m(8) | m(0)) >>> 0;
}

/** WCAG 2.2's relative luminance. */
export function luminance(hex) {
  const lin = (v) => {
    const c = v / 255;
    return c <= 0.04045 ? c / 12.92 : Math.pow((c + 0.055) / 1.055, 2.4);
  };
  return 0.2126 * lin(ch(hex, 16)) + 0.7152 * lin(ch(hex, 8)) + 0.0722 * lin(ch(hex, 0));
}

const ratio = (a, b) => (Math.max(a, b) + 0.05) / (Math.min(a, b) + 0.05);

/** A colour's hue in degrees, as AppKit's getHue gives it. */
function hueOf(hex) {
  const r = ch(hex, 16) / 255, g = ch(hex, 8) / 255, b = ch(hex, 0) / 255;
  const hi = Math.max(r, g, b), lo = Math.min(r, g, b);
  if (hi === lo) return 0;
  let h;
  if (hi === r) h = (g - b) / (hi - lo);
  else if (hi === g) h = 2 + (b - r) / (hi - lo);
  else h = 4 + (r - g) / (hi - lo);
  h *= 60;
  return h < 0 ? h + 360 : h;
}

/** The sRGB colour of a hue in degrees, a saturation and a brightness. */
function fromHSB(hDeg, s, v) {
  const h = (((hDeg % 360) + 360) % 360) / 60;
  const i = Math.floor(h), f = h - i;
  const p = v * (1 - s), q = v * (1 - s * f), t = v * (1 - s * (1 - f));
  const [r, g, b] = [[v, t, p], [q, v, p], [p, v, t], [p, q, v], [t, p, v], [v, p, q]][i % 6];
  const c = (x) => Math.round(x * 255);
  return ((c(r) << 16) | (c(g) << 8) | c(b)) >>> 0;
}

/** A slot's hue: a fan over the half of the wheel opposite the accent, that skips the needs red. */
function agentHue(slot, s) {
  const start = hueOf(s.accent) + 90;
  let red = (hueOf(s.needs) - start) % 360;
  if (red < 0) red += 360;
  if (red >= 270) red -= 360;
  const from = Math.max(0, red - 30), gap = Math.max(0, Math.min(180, red + 30) - from);
  let along = (slot % SLOTS) * (180 - gap) / (SLOTS - 1);
  if (gap > 0 && along >= from) along += gap;
  return (start + along) % 360;
}

/** A working card's ground: the card washed with the hue, keeping its muted text at 4.6:1. */
function agentWash(slot, s, dark) {
  const hue = agentHue(slot, s);
  const vivid = fromHSB(hue, 0.62, dark ? 0.6 : 1);
  const muted = luminance(s.muted), brightest = luminance(fromHSB(hue, 0.62, 1));
  const fits = (t) => {
    const l = luminance(mix(s.card, vivid, t));
    return ratio(l, muted) >= 4.6 && (!dark || ratio(l, brightest) >= 3.2);
  };
  let lo = 0, hi = dark ? 0.45 : 0.2;
  if (fits(hi)) return mix(s.card, vivid, hi);
  for (let k = 0; k < 14; k++) {
    const mid = (lo + hi) / 2;
    if (fits(mid)) lo = mid; else hi = mid;
  }
  return mix(s.card, vivid, lo);
}

/** Searches the brightness that keeps want:1 on every ground: past 1 it pales toward white. */
function searchHue(hue, grounds, want, dark) {
  const at = (x) => (x <= 1 ? fromHSB(hue, 0.62, x) : fromHSB(hue, 0.62 * (2 - x), 1));
  let lo = 0, hi = dark ? 2 : 1;
  for (let k = 0; k < 14; k++) {
    const mid = (lo + hi) / 2;
    const l = luminance(at(mid));
    if (grounds.every((g) => ratio(l, g) >= want)) {
      if (dark) hi = mid; else lo = mid;
    } else if (dark) lo = mid; else hi = mid;
  }
  return at(dark ? hi : lo);
}

/** A working card's mark: 3.2:1 on its wash and on the selection. */
function agentMark(slot, s, dark) {
  const grounds = [agentWash(slot, s, dark), s.selection].map(luminance);
  return searchHue(agentHue(slot, s), grounds, 3.2, dark);
}

/** The agent's colour as text, away from its card: 4.6:1 on every ground. */
function agentInk(slot, s, dark) {
  const grounds = [s.sidebar, s.ground, s.card, s.panel].map(luminance);
  return searchHue(agentHue(slot, s), grounds, 4.6, dark);
}

const hex = (n) => '#' + n.toString(16).padStart(6, '0');

/** CSS rules giving .slot0…5 their colours in light and dark mode, and on the dark bars. */
export function agentRules() {
  const rules = [];
  const block = (s, dark) => {
    const out = [];
    for (let i = 0; i < SLOTS; i++) {
      const wash = agentWash(i, s, dark);
      out.push([i, `--a-mark:${hex(agentMark(i, s, dark))};--a-wash:${hex(wash)};--a-wash2:${hex(mix(s.card, wash, 0.5))};--a-ink:${hex(agentInk(i, s, dark))}`]);
    }
    return out;
  };
  const light = block(PALETTE.light, false), dark = block(PALETTE.dark, true);
  for (const [i, v] of light) rules.push(`.slot${i}{${v}}`);
  rules.push('@media (prefers-color-scheme: dark){' + dark.map(([i, v]) => `.slot${i}{${v}}`).join('') + '}');
  // The bars are dark in both modes, so their marks take the dark set.
  for (const [i, v] of dark) rules.push(`.on-dark .slot${i},.on-dark.slot${i}{${v}}`);
  return rules;
}

// ---- Short forms ----------------------------------------------------------

/** The time, unless the core had none (Go's zero time). */
export function when(iso) {
  if (!iso) return null;
  const t = Date.parse(iso);
  return Number.isFinite(t) && t > 0 ? t : null;
}

/** An age: now, 12m, 3h, 2d. */
export function age(iso, now = Date.now()) {
  const t = when(iso);
  if (t == null) return '';
  const s = Math.max(0, Math.floor((now - t) / 1000));
  if (s < 60) return 'now';
  if (s < 3600) return Math.floor(s / 60) + 'm';
  if (s < 86400) return Math.floor(s / 3600) + 'h';
  return Math.floor(s / 86400) + 'd';
}

/** An age for VoiceOver, which would read 3m as metres. */
export function ageSpoken(iso, now = Date.now()) {
  const t = when(iso);
  if (t == null) return '';
  const s = Math.max(0, Math.floor((now - t) / 1000));
  const n = (v, unit) => `${v} ${unit}${v === 1 ? '' : 's'} ago`;
  if (s < 60) return 'just now';
  if (s < 3600) return n(Math.floor(s / 60), 'minute');
  if (s < 86400) return n(Math.floor(s / 3600), 'hour');
  return n(Math.floor(s / 86400), 'day');
}

/** How long a call took: 0.4 s, 40 s, 3 min, 1 h 5 min. */
export function took(ms) {
  const s = ms / 1000;
  if (s < 10) return s.toFixed(1) + ' s';
  if (s < 60) return Math.round(s) + ' s';
  if (s < 3600) return Math.floor(s / 60) + ' min';
  const m = Math.floor(s / 60);
  return m % 60 === 0 ? `${m / 60} h` : `${Math.floor(m / 60)} h ${m % 60} min`;
}

/** A token count in short: 940, 4.2k, 48k, 1.3M. */
export function count(n) {
  if (n < 1000) return String(n);
  if (n < 1e6) return (n < 10000 ? (n / 1000).toFixed(1) : (n / 1000).toFixed(0)) + 'k';
  return (n < 1e7 ? (n / 1e6).toFixed(1) : (n / 1e6).toFixed(0)) + 'M';
}

export function tokenTotal(t) {
  return t ? (t.input || 0) + (t.output || 0) + (t.cache_read || 0) + (t.cache_write || 0) : 0;
}

/** A path's last part, from its first line. */
export function base(path) {
  const first = String(path || '').split('\n')[0].replace(/\/+$/, '');
  return first.split('/').pop() || first;
}

/** A clock time, such as 14:05, or a weekday and time when it is not today. */
export function clock(t, now = Date.now()) {
  const d = new Date(t);
  const time = d.toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' });
  if (new Date(now).toDateString() === d.toDateString()) return time;
  if (Math.abs(now - t) < 6 * 86400e3) return d.toLocaleDateString([], { weekday: 'short' }) + ' ' + time;
  return d.toLocaleDateString([], { month: 'short', day: 'numeric' }) + ' ' + time;
}

// ---- Irie's mark ------------------------------------------------------------

/** The sun rising over two waves (Theme.swift, SunMark), on a 32-point grid. */
export function sun(cls = 'sun') {
  const rays = [[16, 2.5, 16, 5], [8, 5.6, 9.6, 7.4], [24, 5.6, 22.4, 7.4], [4.2, 13, 6.6, 13], [25.4, 13, 27.8, 13]]
    .map(([a, b, c, d]) => `M${a} ${b}L${c} ${d}`).join('');
  const wave = (y, from, to) => {
    let d = `M${from} ${y}`, x = from, up = true;
    while (x < to - 1e-9) {
      const next = Math.min(x + 4.3, to);
      d += `Q${((x + next) / 2).toFixed(2)} ${(up ? y - 2.2 : y + 0.6).toFixed(2)} ${next.toFixed(2)} ${y}`;
      x = next;
      up = !up;
    }
    return d;
  };
  return `<svg class="${cls}" viewBox="0 0 32 32" aria-hidden="true" focusable="false">` +
    `<path d="${rays}" stroke="var(--sun)" stroke-width="2" stroke-linecap="round" fill="none"/>` +
    '<circle cx="16" cy="13.5" r="5.5" fill="var(--sun)"/>' +
    `<path d="${wave(21.6, 3, 29)}${wave(26.6, 6.5, 26)}" stroke="var(--waves)" stroke-width="2" stroke-linecap="round" fill="none"/></svg>`;
}
