// Irie for iPhone: the sessions of every machine this phone is paired with,
// each machine's gateway (`irie remote serve`) relaying its core's ops and
// frames. Like every Irie client it draws what the core says, sends what is
// typed to `input`, and shows the core's words as they are (docs/CORE.md).

import { h, $, ICON, sheet, actions, ask, toast, closeSheet, openSheet } from './js/ui.js';
import * as L from './js/look.js';
import { esc } from './js/md.js';
import { loadMachines, saveMachines, call, pair, info, forget, normalize, host, Stream } from './js/net.js';
import { SessionView, grow, looseWords } from './js/conv.js';
import * as Push from './js/push.js';

const params = new URLSearchParams(location.search);
const root = document.documentElement;
const app = document.getElementById('app');

// ---- The viewport ---------------------------------------------------------
// The app is as tall as the visual viewport, so the bottom bar sits right
// above the keyboard; without a keyboard it keeps clear of the home bar.

function fit() {
  const vv = window.visualViewport;
  if (!vv || Math.abs(vv.scale - 1) > 0.01) return;
  root.style.setProperty('--vvh', vv.height + 'px');
  root.style.setProperty('--vvt', vv.offsetTop + 'px');
  root.classList.toggle('kb', root.clientHeight - vv.height > 120);
}
if (window.visualViewport) {
  visualViewport.addEventListener('resize', fit);
  visualViewport.addEventListener('scroll', fit);
}
window.addEventListener('resize', fit);
fit();

// The agents' own colours, worked out once for both modes.
(function agentColours() {
  const rules = L.agentRules();
  const link = document.querySelector('link[href$="app.css"]');
  const add = () => {
    try {
      const sheet = link.sheet;
      for (const r of rules) sheet.insertRule(r, sheet.cssRules.length);
    } catch {
      document.head.append(h('style', { text: rules.join('\n') }));
    }
  };
  if (link && link.sheet) add();
  else if (link) link.addEventListener('load', add, { once: true });
  else document.head.append(h('style', { text: rules.join('\n') }));
})();

// ---- State ----------------------------------------------------------------

const ua = navigator.userAgent;
const IOS = /iPhone|iPad|iPod/.test(ua) || (navigator.platform === 'MacIntel' && navigator.maxTouchPoints > 1);
const MOBILE = IOS || /Android/i.test(ua);
const STANDALONE = navigator.standalone === true || matchMedia('(display-mode: standalone)').matches ||
  matchMedia('(display-mode: fullscreen)').matches || params.get('standalone') === '1';
const DEVICE = /iPad/.test(ua) ? 'iPad' : /iPhone/.test(ua) ? 'iPhone' : /Android/i.test(ua) ? 'Android phone' : 'Browser';

const read = (key, fallback) => {
  try {
    return JSON.parse(localStorage.getItem(key)) ?? fallback;
  } catch {
    return fallback;
  }
};

const S = {
  machines: loadMachines(),
  conns: new Map(),
  view: null,
  pushed: false,
  filter: false,
  parkedOpen: read('irie.parkedOpen', {}),
  closedOpen: {},
  task: read('irie.task', { mid: '', by: {} }),
  welcome: !STANDALONE && MOBILE && !localStorage.getItem('irie.browser'),
  screen: '',
  pairTarget: null,
  hiddenAt: 0,
};

/** A paired machine and what its core said last. */
class Conn {
  constructor(m) {
    this.m = m;
    this.status = 'connecting';
    this.sessions = new Map();
    this.header = {};
    this.history = { undo: '', redo: '' };
    this.closed = [];
    this.loaded = false;
    this.features = null;
    this.info = null;
    this.seen = 0;
  }

  start() {
    this.status = 'connecting';
    this.stream = new Stream(this.m, {}, {
      frame: (f) => this.frame(f),
      state: (st) => {
        if (this.status === 'unpaired') return;
        this.status = st;
        if (st === 'connected') {
          this.seen = Date.now();
          this.hello();
        }
        invalidate();
      },
      unpaired: (msg) => unpaired(this, msg),
    });
    this.stream.start();
    this.fetchInfo();
  }

  stop() {
    if (this.stream) this.stream.stop();
  }

  frame(f) {
    switch (f.t) {
      case 'snapshot':
        this.sessions = new Map((f.sessions || []).map((s) => [s.id, s]));
        if (f.header) this.header = f.header;
        this.loaded = true;
        break;
      case 'upsert':
        if (f.session) this.sessions.set(f.session.id, f.session);
        break;
      case 'remove':
        this.sessions.delete(f.id);
        break;
      case 'header':
        this.header = f.header || {};
        break;
      case 'history':
        this.history = { undo: f.undo || '', redo: f.redo || '' };
        break;
      case 'closed':
        this.closed = f.closed || [];
        break;
      default:
        return;
    }
    this.seen = Date.now();
    changed(this);
  }

  async hello() {
    try {
      const r = await call(this.m, 'hello', { client: 'irie-phone', api: 1 });
      this.features = Array.isArray(r.features) && r.features.length ? new Set(r.features) : null;
      this.version = r.version;
      invalidate();
    } catch {
      // An older core still takes the ops; offer them all.
    }
  }

  async fetchInfo() {
    try {
      this.info = await info(this.m.url);
      if (this.info.name && this.info.name !== this.m.name) {
        this.m.name = this.info.name;
        saveMachines(S.machines);
      }
      invalidate();
    } catch {
      // Offline: its status says so.
    }
  }

  /** Whether its core serves an op, by hello's features. */
  can(op) {
    return !this.features || this.features.has(op);
  }

  /** Its rows in the core's order, the parked ones after the rest. */
  rows() {
    const all = [...this.sessions.values()].sort((a, b) =>
      (a.order ?? 1e9) - (b.order ?? 1e9) || String(a.created).localeCompare(String(b.created)) || a.id.localeCompare(b.id));
    return { live: all.filter((s) => !s.parked), parked: all.filter((s) => s.parked) };
  }

  get redOnDone() {
    return !!(this.header && this.header.red_on_done);
  }
}

function conns() {
  return S.machines.map((m) => S.conns.get(m.id)).filter(Boolean);
}

function startAll() {
  for (const m of S.machines) {
    if (!S.conns.has(m.id)) {
      const c = new Conn(m);
      S.conns.set(m.id, c);
      c.start();
    }
  }
  for (const [id, c] of S.conns) {
    if (!S.machines.some((m) => m.id === id)) {
      c.stop();
      S.conns.delete(id);
    }
  }
}

function changed(conn) {
  invalidate();
  if (S.view && S.view.conn === conn) S.view.rowChanged();
}

let scheduled = false;
function invalidate() {
  if (scheduled) return;
  scheduled = true;
  requestAnimationFrame(() => {
    scheduled = false;
    render();
  });
}

// ---- Ops ------------------------------------------------------------------

function fail(conn, e) {
  if (e.status === 401 || e.code === 'unpaired') unpaired(conn, e.message);
  else toast(e.message, { error: true });
}

async function op(conn, name, args, { undo, done } = {}) {
  try {
    const r = await call(conn.m, name, args);
    if (undo) toast(undo, { action: { label: 'Undo', run: () => undoLast(conn) } });
    else if (done) toast(done);
    return r;
  } catch (e) {
    fail(conn, e);
    return null;
  }
}

async function undoLast(conn) {
  try {
    afterHistory(conn, 'undo', await call(conn.m, 'undo', {}));
  } catch (e) {
    fail(conn, e);
  }
}

/** What an undo or redo did; a question the core asks goes to an action sheet. */
function afterHistory(conn, name, r) {
  if (r.did === 'asked' && r.choice) {
    const c = r.choice;
    actions({
      title: c.question,
      items: (c.options || []).map((o) => ({
        label: o.label + (o.recommended ? ' (recommended)' : ''),
        note: o.detail,
        run: async () => {
          try {
            afterHistory(conn, name, await call(conn.m, name, { choice_id: c.id, choice: o.key }));
          } catch (e) {
            fail(conn, e);
          }
        },
      })),
    });
    return;
  }
  const word = { undone: 'Undone', redone: 'Redone', settled: 'It was already so', skipped: 'Skipped', cancelled: 'Nothing changed' }[r.did] || 'Done';
  toast(r.label ? `${word}: ${r.label}` : word);
}

async function closeSession(conn, s) {
  try {
    await call(conn.m, 'close', { session: s.id });
    if (S.view && S.view.conn === conn && S.view.sid === s.id) back();
    toast(`Closed “${s.name}”`, { action: { label: 'Undo', run: () => undoLast(conn) } });
  } catch (e) {
    fail(conn, e);
  }
}

async function reopen(conn, id) {
  try {
    const r = await call(conn.m, 'reopen', { session: id });
    toast(`Reopened “${(r.session && r.session.name) || 'the session'}”`);
  } catch (e) {
    fail(conn, e);
  }
}

/** What can be done to a session, for its action sheet and its long press. */
function sessionActions(conn, s, { inSession = false } = {}) {
  const frozen = s.state === 'frozen' || !!s.frozen;
  const lk = L.look(s);
  const ended = lk === 'exited' || lk === 'exitedError' || lk === 'failed';
  const items = [];
  if (!inSession) items.push({ label: 'Open', run: () => openSession(conn.m.id, s.id) });
  if (conn.can('park')) {
    items.push(s.parked
      ? { label: 'Unpark', note: 'Back to its place in the list', run: () => op(conn, 'unpark', { session: s.id }, { undo: `Unparked “${s.name}”` }) }
      : { label: 'Park', note: 'Out of the way, still running', run: () => op(conn, 'park', { session: s.id }, { undo: `Parked “${s.name}”` }) });
  }
  if (!ended && conn.can('freeze')) {
    items.push(frozen
      ? { label: 'Thaw', note: 'It carries on where it stopped', run: () => op(conn, 'thaw', { session: s.id }, { done: `Thawed “${s.name}”` }) }
      : { label: 'Freeze', note: 'Stops it at once, every process', run: () => op(conn, 'freeze', { session: s.id }, { done: `Froze “${s.name}”` }) });
  }
  if (!ended && conn.can('priority')) {
    items.push(s.priority
      ? { label: 'Put back', note: 'Among the rest again', run: () => op(conn, 'priority', { session: s.id, level: 0 }, { done: `Put “${s.name}” back` }) }
      : { label: 'Promote', note: 'To the top, and the machine first', run: () => op(conn, 'priority', { session: s.id, level: 1 }, { done: `Promoted “${s.name}”` }) });
  }
  if (conn.can('rate')) {
    items.push({ row: [
      { label: 'Rate good', checked: s.rated === 'good', run: () => op(conn, 'rate', { session: s.id, verdict: 'good' }, { done: 'Rated good. Irie’s router learns from it.' }) },
      { label: 'Rate bad', checked: s.rated === 'bad', run: () => op(conn, 'rate', { session: s.id, verdict: 'bad' }, { done: 'Rated bad. Irie’s router learns from it.' }) },
    ] });
  }
  if (conn.can('rename')) {
    items.push({
      label: 'Rename…',
      run: () => ask({
        title: 'Rename',
        value: s.name,
        okLabel: 'Rename',
        run: async (name) => {
          try {
            await call(conn.m, 'rename', { session: s.id, name });
          } catch (e) {
            if (e.status === 401) unpaired(conn, e.message);
            throw e;
          }
        },
      }),
    });
  }
  if (inSession && !ended && lk !== 'starting' && conn.can('keys')) {
    items.push({ label: 'Stop the turn', note: 'As Esc in its terminal', run: () => op(conn, 'keys', { session: s.id, keys: ['Escape'] }, { done: 'Sent Esc.' }) });
  }
  if (conn.can('close')) {
    items.push({
      label: 'Close',
      destructive: true,
      confirm: { title: `Close “${s.name}”?`, message: 'Its agent stops and its card goes. Undo brings it back.', label: 'Close' },
      run: () => closeSession(conn, s),
    });
  }
  return items;
}

// ---- Pairing state --------------------------------------------------------

function unpaired(conn, msg) {
  if (conn.status === 'unpaired') return;
  conn.status = 'unpaired';
  conn.unpairedMsg = msg || '';
  conn.stop();
  if (S.view && S.view.conn === conn) S.view.stream.stop();
  invalidate();
  toast(`${conn.m.name} no longer knows this phone.`, { error: true, action: { label: 'Pair again', run: () => pairAgain(conn) } });
}

function pairAgain(conn) {
  closeSheet();
  S.pairTarget = { url: conn.m.url, name: conn.m.name, again: true };
  show();
}

/** Keeps a machine paired with its token, replacing an older pairing of it. */
function remember(url, r) {
  const m = { id: r.machine.id, name: r.machine.name || host(url), url, token: r.token };
  const old = S.conns.get(m.id);
  if (old) {
    old.stop();
    S.conns.delete(m.id);
  }
  S.machines = S.machines.filter((x) => x.id !== m.id && x.url !== url);
  S.machines.push(m);
  saveMachines(S.machines);
  startAll();
  // A machine paired now has none of this phone's notifications yet.
  forgetSent(m.id);
  if (pushReady()) syncPush([S.conns.get(m.id)]);
  return m;
}

// ---- Screens --------------------------------------------------------------

const refs = {};

function show() {
  let want;
  if (S.welcome) want = 'welcome';
  else if (S.pairTarget || !S.machines.length) want = 'pair';
  else want = 'home';
  if (want !== S.screen || want === 'pair') {
    if (S.screen === 'home' && want !== 'home') closeView(true);
    for (const el of app.querySelectorAll(':scope > .screen')) el.remove();
    S.screen = want;
    if (want === 'welcome') app.prepend(buildWelcome());
    else if (want === 'pair') app.prepend(buildPair(S.pairTarget || { url: location.origin }));
    else {
      app.prepend(refs.home || (refs.home = buildHome()));
      startAll();
      route();
    }
    trackBottom();
  }
  invalidate();
}

function render() {
  if (S.screen === 'home') renderHome();
  if (refs.machinesSheet) refs.machinesSheet.render();
}

// ---- Welcome: not on the Home Screen yet ----------------------------------

function buildWelcome() {
  const step = (n, icon, title, text) => h('li', { class: 'step' },
    h('span', { class: 'step-n', text: String(n) }),
    h('div', { class: 'step-body' }, h('div', { class: 'step-title' }, title, icon ? h('span', { class: 'step-ic', html: icon }) : null), h('div', { class: 'step-text', text })));
  const steps = IOS
    ? [
        step(1, ICON.share, 'Tap Share', 'In Safari’s toolbar, or under ⋯ beside the address.'),
        step(2, ICON.add, 'Choose Add to Home Screen', 'Scroll down the list if you don’t see it, then tap Add.'),
        step(3, null, 'Open Irie from your Home Screen', 'Pair it there with the code your machine shows.'),
      ]
    : [
        step(1, null, 'Open your browser’s menu', 'The ⋮ or ⋯ button by the address bar.'),
        step(2, ICON.add, 'Choose Install app or Add to Home screen', 'Then confirm.'),
        step(3, null, 'Open Irie from your Home Screen', 'Pair it there with the code your machine shows.'),
      ];
  return h('section', { class: 'screen welcome', 'aria-label': 'Add Irie to your Home Screen' },
    h('div', { class: 'hero on-dark' },
      h('img', { class: 'app-icon', src: 'icon-180.png', alt: '', width: '84', height: '84' }),
      h('h1', { class: 'hero-title', text: 'Put Irie on your Home Screen' }),
      h('p', { class: 'hero-text', text: 'It opens full screen like an app, and stays paired with your machines.' })),
    h('div', { class: 'scroll welcome-body' },
      h('ol', { class: 'steps' }, steps),
      h('p', { class: 'fine', text: 'A Home Screen app keeps its own pairing, so pair it after you add it.' }),
      h('button', {
        class: 'linkish center',
        text: 'Use it in the browser instead',
        onclick: () => {
          localStorage.setItem('irie.browser', '1');
          S.welcome = false;
          show();
        },
      })));
}

// ---- Pairing ----------------------------------------------------------------

function buildPair(target) {
  const again = !!target.again;
  const name = h('span', { text: target.name || 'this machine' });
  const problem = h('div', { class: 'problem', role: 'alert' });
  const code = h('input', {
    class: 'code-in', type: 'text', inputmode: 'numeric', autocomplete: 'one-time-code', pattern: '[0-9]*',
    maxlength: '6', placeholder: '000000', 'aria-label': 'Pairing code', enterkeyhint: 'go', autocorrect: 'off', spellcheck: 'false',
  });
  const go = h('button', { class: 'btn primary wide', type: 'submit', text: 'Pair', disabled: true });
  const form = h('form', { class: 'pair-form' }, code, problem, go);
  let busy = false;
  const submit = async () => {
    const c = code.value.replace(/\D/g, '');
    if (c.length !== 6 || busy) return;
    busy = true;
    go.disabled = true;
    go.textContent = 'Pairing…';
    problem.textContent = '';
    try {
      const r = await pair(target.url, c, DEVICE);
      const m = remember(target.url, r);
      S.pairTarget = null;
      code.blur();
      show();
      toast(`Paired with ${m.name}.`);
    } catch (e) {
      problem.textContent = e.message;
      go.textContent = 'Pair';
      go.disabled = false;
      code.select();
    } finally {
      busy = false;
    }
  };
  code.addEventListener('input', () => {
    const v = code.value.replace(/\D/g, '').slice(0, 6);
    if (v !== code.value) code.value = v;
    go.disabled = v.length !== 6;
    problem.textContent = '';
    if (v.length === 6) submit();
  });
  form.addEventListener('submit', (e) => {
    e.preventDefault();
    submit();
  });
  const reach = h('div', { class: 'reach', hidden: true });
  info(target.url).then((i) => {
    name.textContent = i.name || name.textContent;
  }).catch((e) => {
    reach.hidden = false;
    reach.replaceChildren(h('span', { text: e.message }), h('button', {
      class: 'linkish', text: 'Try again',
      onclick: () => {
        S.pairTarget = target;
        show();
      },
    }));
  });
  const cancel = again || S.machines.length
    ? h('button', {
        class: 'linkish center', text: 'Not now',
        onclick: () => {
          S.pairTarget = null;
          show();
        },
      })
    : null;
  setTimeout(() => code.focus(), 350);
  return h('section', { class: 'screen pair', 'aria-label': 'Pair' },
    h('div', { class: 'hero on-dark' },
      h('div', { class: 'hero-mark', html: L.sun('sun') }),
      h('h1', { class: 'hero-title' }, again ? 'Pair again with ' : 'Pair with ', name),
      h('p', { class: 'hero-text', text: again ? 'It no longer knows this phone. A new code pairs them again.' : 'Irie talks to your machine directly, over your Wi-Fi.' })),
    h('div', { class: 'scroll pair-body' },
      h('p', { class: 'lead' }, 'On the machine, run ', h('code', { text: 'irie remote pair' }), '. It shows a code for 10 minutes.'),
      reach, form, cancel));
}

// ---- Home -----------------------------------------------------------------

function buildHome() {
  refs.needsPill = h('button', { class: 'needs-pill', hidden: true, onclick: () => setFilter(!S.filter) });
  refs.chips = h('div', { class: 'machine-chips', role: 'list' });
  const header = h('header', { class: 'bar home-bar on-dark' },
    h('div', { class: 'title-row' },
      h('h1', { class: 'large-title' }, h('span', { class: 'title-mark', html: L.sun('sun') }), 'Irie'),
      refs.needsPill),
    refs.chips);
  refs.filterNote = h('div', { class: 'filter-note', hidden: true });
  refs.sections = h('div', { class: 'sections' });
  refs.empty = h('div', { class: 'empty', hidden: true });
  refs.homeScroll = h('div', { class: 'scroll home-scroll' }, refs.filterNote, refs.sections, refs.empty);
  refs.homeScroll.addEventListener('scroll', () => {
    if (S.openRow) setOpen(S.openRow, false);
  }, { passive: true });
  const el = h('section', { class: 'screen home', 'aria-label': 'Sessions' }, header, refs.homeScroll, buildTaskBar());
  return el;
}

function setFilter(on) {
  S.filter = on;
  refs.homeScroll.scrollTop = 0;
  invalidate();
}

function statusWords(c) {
  switch (c.status) {
    case 'connected': return 'Connected';
    case 'connecting': return 'Connecting…';
    case 'unpaired': return 'Not paired';
    default: return 'Offline';
  }
}

function renderHome() {
  const now = Date.now();
  const all = conns();
  const multi = all.length > 1;
  // The needs pill: how many need you, on every machine.
  let loudN = 0;
  for (const c of all) for (const s of c.sessions.values()) if (L.loud(L.look(s))) loudN++;
  if (S.filter && loudN === 0) S.filter = false;
  refs.needsPill.hidden = loudN === 0;
  const pillText = S.filter ? 'Show all' : `${loudN} need${loudN === 1 ? 's' : ''} you`;
  if (refs.needsPill.textContent !== pillText) {
    refs.needsPill.replaceChildren(S.filter ? '' : h('span', { class: 'pill-dot', html: L.glyph('needs') }), pillText);
    refs.needsPill.setAttribute('aria-pressed', String(S.filter));
  }
  // The machines' chips.
  const chipsKey = all.map((c) => c.m.id + c.m.name + c.status).join('|');
  if (refs.chipsKey !== chipsKey) {
    refs.chipsKey = chipsKey;
    refs.chips.replaceChildren(...all.map((c) => h('button', {
      class: 'mchip ' + c.status, role: 'listitem', 'aria-label': `${c.m.name}: ${statusWords(c)}. Shows the machines.`, onclick: openMachines,
    }, h('span', { class: 'dot ' + c.status, 'aria-hidden': 'true' }), h('span', { class: 'mchip-name', text: c.m.name }),
    c.status === 'connected' ? null : h('span', { class: 'mchip-status', text: statusWords(c) }))),
    h('button', { class: 'mchip add', 'aria-label': 'Machines', onclick: openMachines, html: ICON.machine }));
  }
  refs.filterNote.hidden = !S.filter;
  if (S.filter) {
    refs.filterNote.replaceChildren(h('span', { text: `Only the ${loudN} that need${loudN === 1 ? 's' : ''} you` }), h('button', { class: 'linkish', text: 'Show all', onclick: () => setFilter(false) }));
  }
  // A section per machine, in the order they were paired.
  const keep = new Set();
  let any = false;
  for (const c of all) {
    keep.add(c.m.id);
    let sec = refs.sections.querySelector(`:scope > [data-mid="${CSS.escape(c.m.id)}"]`);
    // A machine paired again is a new connection: its cards start afresh,
    // so no gesture keeps the old one's token.
    if (sec && sec._conn !== c) {
      const fresh = buildSection(c);
      sec.replaceWith(fresh);
      sec = fresh;
    }
    if (!sec) {
      sec = buildSection(c);
      refs.sections.append(sec);
    }
    if (renderSection(sec, c, multi, now)) any = true;
  }
  for (const sec of [...refs.sections.children]) if (!keep.has(sec.dataset.mid)) sec.remove();
  // Keep the sections in the machines' order.
  all.forEach((c, i) => {
    const sec = refs.sections.querySelector(`:scope > [data-mid="${CSS.escape(c.m.id)}"]`);
    if (refs.sections.children[i] !== sec) refs.sections.insertBefore(sec, refs.sections.children[i] || null);
  });
  renderEmpty(all, any);
  renderTaskChips();
}

function buildSection(c) {
  const sec = h('section', { class: 'msec', 'data-mid': c.m.id });
  sec._conn = c;
  sec._head = h('h2', { class: 'msec-head' });
  sec._note = h('div', { class: 'msec-note', hidden: true });
  sec._live = h('div', { class: 'cards' });
  sec._parkedHead = h('button', { class: 'divider', 'aria-expanded': 'false', onclick: () => {
    S.parkedOpen[c.m.id] = !S.parkedOpen[c.m.id];
    localStorage.setItem('irie.parkedOpen', JSON.stringify(S.parkedOpen));
    invalidate();
  } });
  sec._parked = h('div', { class: 'cards parked' });
  sec._closedHead = h('button', { class: 'divider', 'aria-expanded': 'false', onclick: () => {
    S.closedOpen[c.m.id] = !S.closedOpen[c.m.id];
    invalidate();
  } });
  sec._closed = h('div', { class: 'closed-list' });
  sec.append(sec._head, sec._note, sec._live, sec._parkedHead, sec._parked, sec._closedHead, sec._closed);
  sec._rows = new Map();
  return sec;
}

/** Draws one machine's section; says whether it shows any card. */
function renderSection(sec, c, multi, now) {
  sec._head.hidden = !multi;
  if (multi) {
    const key = c.m.name + c.status + c.sessions.size;
    if (sec._headKey !== key) {
      sec._headKey = key;
      sec._head.replaceChildren(h('span', { class: 'dot ' + c.status, 'aria-hidden': 'true' }), h('span', { text: c.m.name }),
        h('span', { class: 'cnt', text: String(c.sessions.size) }));
    }
  }
  // Offline, connecting or unpaired: say so above what it said last.
  let note = null;
  if (c.status === 'unpaired') {
    note = h('div', { class: 'banner loud' }, h('span', { text: `${c.m.name} no longer knows this phone.` }),
      h('button', { class: 'btn small primary', text: 'Pair again', onclick: () => pairAgain(c) }));
  } else if (c.status === 'offline' && c.loaded) {
    note = h('div', { class: 'banner' }, h('span', { text: `Can’t reach ${c.m.name}. This is what it said ${L.ageSpoken(new Date(c.seen).toISOString(), now)}.` }),
      h('button', { class: 'btn small', text: 'Retry', onclick: () => c.stream && c.stream.kick() }));
  }
  const noteKey = c.status + (c.loaded ? 1 : 0) + Math.floor((now - c.seen) / 60000);
  if (sec._noteKey !== noteKey) {
    sec._noteKey = noteKey;
    sec._note.hidden = !note;
    sec._note.replaceChildren(...(note ? [note] : []));
  }
  sec.classList.toggle('stale', c.status !== 'connected');
  const { live, parked } = c.rows();
  const pick = (list) => (S.filter ? list.filter((s) => L.loud(L.look(s))) : list);
  const liveShown = c.status === 'unpaired' ? [] : pick(live);
  const parkedShown = c.status === 'unpaired' ? [] : pick(parked);
  const open = S.filter || !!S.parkedOpen[c.m.id];
  placeRows(sec, sec._live, c, liveShown, now);
  placeRows(sec, sec._parked, c, open ? parkedShown : [], now);
  // The Parked divider: its size, and in red how many of it need you.
  const parkedLoud = parked.filter((s) => L.loud(L.look(s))).length;
  sec._parkedHead.hidden = !parkedShown.length;
  const pk = `${parkedShown.length}|${parkedLoud}|${open}`;
  if (sec._pk !== pk) {
    sec._pk = pk;
    sec._parkedHead.setAttribute('aria-expanded', String(open));
    sec._parkedHead.replaceChildren(h('span', { class: 'chev', html: ICON.chevron }), h('span', { class: 'dv-title' + (parkedLoud ? ' loud' : ''), text: 'Parked' }),
      parkedLoud && !open ? h('span', { class: 'dv-loud', text: `· ${parkedLoud} need${parkedLoud === 1 ? 's' : ''} you` }) : null,
      h('span', { class: 'cnt', text: String(parkedShown.length) }));
  }
  // Closed: what reopen can bring back.
  const closed = S.filter || c.status === 'unpaired' ? [] : c.closed;
  const cOpen = !!S.closedOpen[c.m.id];
  sec._closedHead.hidden = !closed.length;
  const ck = closed.map((x) => x.id).join() + cOpen + Math.floor(now / 60000);
  if (sec._ck !== ck) {
    sec._ck = ck;
    sec._closedHead.setAttribute('aria-expanded', String(cOpen));
    sec._closedHead.replaceChildren(h('span', { class: 'chev', html: ICON.chevron }), h('span', { class: 'dv-title', text: 'Closed' }), h('span', { class: 'cnt', text: String(closed.length) }));
    sec._closed.replaceChildren(...(cOpen ? closed.map((x) => h('div', { class: 'closed-item' },
      h('div', { class: 'ci-body' }, h('div', { class: 'ci-name', text: x.name }),
        h('div', { class: 'ci-when', text: (x.auto ? 'Closed itself ' : 'Closed ') + L.ageSpoken(x.closed, now) })),
      h('button', { class: 'btn small', text: 'Reopen', onclick: () => reopen(c, x.id) }))) : []));
  }
  sec.hidden = S.filter && !liveShown.length && !parkedShown.length;
  return liveShown.length + parkedShown.length > 0 || (!!closed.length && !S.filter);
}

/** Puts these rows in this list, in this order, drawing only what changed. */
function placeRows(sec, list, c, rows, now) {
  let cursor = list.firstElementChild;
  const want = new Set(rows.map((s) => s.id));
  for (const s of rows) {
    let row = sec._rows.get(s.id);
    if (!row) {
      row = cardRow(c, s.id);
      sec._rows.set(s.id, row);
    }
    fillCard(row, c, s, now);
    if (row === cursor) cursor = cursor.nextElementSibling;
    else list.insertBefore(row, cursor);
  }
  for (const row of [...list.children]) {
    if (!want.has(row.dataset.id)) {
      row.remove();
      if (!sec._live.contains(row) && !sec._parked.contains(row) && !c.sessions.has(row.dataset.id)) sec._rows.delete(row.dataset.id);
    }
  }
}

/** The second line: what it needs, what it does, or how it ended. */
function status(s, lk) {
  switch (lk) {
    case 'starting': return 'Starting…';
    case 'frozen': return s.freezing ? 'Freezing…' : L.label(s);
    case 'paused': case 'pausing': return L.label(s);
    case 'done': case 'idle': return s.detail || (s.loose ? looseWords(s.loose) : '') || L.label(s);
    default: return s.detail || L.label(s);
  }
}

function cardRow(c, id) {
  const card = h('div', { class: 'card', role: 'button', tabindex: '0' });
  const acts = h('div', { class: 'swipe-actions', 'aria-hidden': 'true' });
  const row = h('div', { class: 'row', 'data-id': id }, acts, card);
  row._card = card;
  row._acts = acts;
  gestures(row, c, id);
  return row;
}

function fillCard(row, c, s, now) {
  const lk = L.look(s);
  const loud = L.loud(lk);
  const quiet = L.quiet(lk) || (lk === 'done' && s.parked);
  const detail = status(s, lk);
  const where = s.repo || s.cwd || s.worktree;
  const meta = [where ? L.base(where) : '', s.branch || '', s.profile ? '+' + s.profile : s.model || ''].filter(Boolean).join(' · ');
  const helpers = (s.subagents || []).slice(0, 2);
  const more = (s.subagents || []).length - helpers.length;
  const age = L.age(s.created, now);
  const sig = JSON.stringify([s.name, lk, detail, meta, age, s.parked, s.priority, !!s.loose, helpers, more, c.redOnDone]);
  if (row._sig === sig) return;
  row._sig = sig;
  const card = row._card;
  card.className = 'card' + (loud ? ' loud' : '') + (quiet ? ' quiet' : '') + (lk === 'working' ? ' working slot' + L.agentSlot(s.id) : '') + (s.loose && !loud ? ' loose' : '');
  card.innerHTML =
    `<span class="${L.markClass(s, c.redOnDone)}">${L.glyph(lk)}</span>` +
    '<div class="c-main">' +
    `<div class="c-top"><span class="c-name">${esc(s.name)}</span>${s.priority ? '<span class="c-prio" title="Promoted">▲</span>' : ''}<span class="c-age">${esc(age)}</span></div>` +
    `<div class="c-detail">${esc(detail)}</div>` +
    (meta ? `<div class="c-meta">${esc(meta)}</div>` : '') +
    (helpers.length ? '<div class="c-helpers">' + helpers.map((a) =>
      `<div class="c-helper${a.needs_you ? ' loud' : ''}"><span class="arrow">↳</span><b>${esc(a.type || 'Helper')}</b> ${esc(a.detail || '')}</div>`).join('') +
      (more > 0 ? `<div class="c-helper">and ${more} more helper${more === 1 ? '' : 's'}</div>` : '') + '</div>' : '') +
    '</div>';
  card.setAttribute('aria-label', [s.name, L.label(s), detail !== L.label(s) ? detail : '', s.priority ? 'promoted' : '', 'started ' + L.ageSpoken(s.created, now)].filter(Boolean).join(', '));
  // The actions under it, for a swipe to the left.
  row._acts.replaceChildren(
    h('button', { class: 'sa park', tabindex: '-1', text: s.parked ? 'Unpark' : 'Park', onclick: () => {
      setOpen(row, false);
      op(c, s.parked ? 'unpark' : 'park', { session: s.id }, { undo: `${s.parked ? 'Unparked' : 'Parked'} “${s.name}”` });
    } }),
    h('button', { class: 'sa close', tabindex: '-1', text: 'Close', onclick: () => {
      setOpen(row, false);
      closeSession(c, s);
    } }));
}

function setOpen(row, open) {
  row.classList.toggle('open', open);
  row._card.style.transform = open ? `translateX(${-row._acts.offsetWidth}px)` : '';
  if (open) {
    if (S.openRow && S.openRow !== row) setOpen(S.openRow, false);
    S.openRow = row;
  } else if (S.openRow === row) S.openRow = null;
}

/** A tap opens the session, a swipe left shows Park and Close, a long press its actions. */
function gestures(row, c, id) {
  const card = row._card;
  let x0 = 0, y0 = 0, dx = 0, base = 0, mode = null, pid = null, timer = null, long = false, kind = '';
  const press = (on) => card.classList.toggle('pressed', on);
  card.addEventListener('pointerdown', (e) => {
    if (e.pointerType === 'mouse' && e.button !== 0) return;
    x0 = e.clientX;
    y0 = e.clientY;
    dx = 0;
    mode = null;
    long = false;
    kind = e.pointerType;
    pid = e.pointerId;
    base = row.classList.contains('open') ? -row._acts.offsetWidth : 0;
    press(true);
    clearTimeout(timer);
    timer = setTimeout(() => {
      if (mode) return;
      long = true;
      mode = 'long';
      press(false);
      const s = c.sessions.get(id);
      if (s) actions({ title: s.name, message: `${L.label(s)} · ${c.m.name}`, items: sessionActions(c, s) });
    }, 520);
  });
  card.addEventListener('pointermove', (e) => {
    if (e.pointerId !== pid || mode === 'long') return;
    const mx = e.clientX - x0, my = e.clientY - y0;
    if (!mode) {
      if (Math.abs(mx) > 8 && Math.abs(mx) > Math.abs(my) * 1.2) {
        mode = 'swipe';
        clearTimeout(timer);
        press(false);
        row.classList.add('swiping');
        try {
          card.setPointerCapture(pid);
        } catch {}
        card.style.transition = 'none';
        if (S.openRow && S.openRow !== row) setOpen(S.openRow, false);
      } else if (Math.abs(my) > 8) {
        mode = 'scroll';
        clearTimeout(timer);
        press(false);
      }
    }
    if (mode === 'swipe') {
      dx = mx;
      const w = row._acts.offsetWidth;
      let x = base + dx;
      if (x < -w) x = -w - (-w - x) * 0.25;
      if (x > 0) x *= 0.25;
      card.style.transform = `translateX(${x}px)`;
    }
  });
  const end = (e) => {
    if (e.pointerId !== pid) return;
    clearTimeout(timer);
    press(false);
    pid = null;
    if (mode === 'swipe') {
      card.style.transition = '';
      setOpen(row, base + dx < -row._acts.offsetWidth / 2);
      // The actions stay drawn until the card has slid back over them.
      setTimeout(() => row.classList.remove('swiping'), 300);
    }
  };
  card.addEventListener('pointerup', end);
  card.addEventListener('pointercancel', (e) => {
    end(e);
    if (mode !== 'swipe') mode = 'scroll';
  });
  card.addEventListener('click', (e) => {
    if (mode === 'swipe' || mode === 'scroll' || long) {
      e.preventDefault();
      return;
    }
    if (row.classList.contains('open')) {
      setOpen(row, false);
      return;
    }
    if (S.openRow) setOpen(S.openRow, false);
    openSession(c.m.id, id);
  });
  card.addEventListener('keydown', (e) => {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      openSession(c.m.id, id);
    }
  });
  card.addEventListener('contextmenu', (e) => {
    e.preventDefault();
    if (kind === 'touch' || long) return;
    const s = c.sessions.get(id);
    if (s) actions({ title: s.name, message: `${L.label(s)} · ${c.m.name}`, items: sessionActions(c, s) });
  });
}

function renderEmpty(all, any) {
  let key, build;
  if (any || S.filter) key = '';
  else if (all.every((c) => c.status === 'unpaired')) key = 'unpaired';
  else if (all.some((c) => c.loaded && c.status === 'connected')) key = 'none';
  else if (all.some((c) => c.status === 'connecting' && !c.loaded)) key = 'connecting';
  else key = 'offline';
  refs.empty.hidden = !key;
  if (refs.emptyKey === key) return;
  refs.emptyKey = key;
  const names = all.map((c) => c.m.name).join(' and ');
  if (key === 'none') {
    build = [h('h2', { class: 'empty-title', text: 'Nothing running yet' }),
      h('p', { class: 'empty-text' }, 'Type a task below, like ', h('q', { text: '@parser fix the flaky timeout test' }), '. Irie starts an agent on it, in its own worktree.')];
  } else if (key === 'connecting') {
    build = [h('h2', { class: 'empty-title', text: `Connecting to ${names}…` })];
  } else if (key === 'offline') {
    build = [h('h2', { class: 'empty-title', text: `Can’t reach ${names}` }),
      h('p', { class: 'empty-text', text: 'Is it awake, and is this phone on the same Wi-Fi?' }),
      h('button', { class: 'btn', text: 'Try again', onclick: kickAll })];
  } else if (key === 'unpaired') {
    build = [h('h2', { class: 'empty-title', text: 'Not paired' }), h('p', { class: 'empty-text', text: 'Pair again with a new code from the machine.' })];
  }
  refs.empty.replaceChildren(...(key ? [h('div', { class: 'empty-mark', html: L.sun('sun big') }), ...build] : []));
}

// ---- The task bar -----------------------------------------------------------

const DRAFT = 'irie.draft';

function buildTaskBar() {
  const ta = h('textarea', { rows: '1', placeholder: 'New task…', 'aria-label': 'New task', enterkeyhint: 'enter', autocapitalize: 'sentences' });
  ta.value = localStorage.getItem(DRAFT) || '';
  const send = h('button', { class: 'send', 'aria-label': 'Start the task', html: ICON.up, disabled: !ta.value.trim() });
  const chips = h('div', { class: 'task-chips' });
  const bar = h('footer', { class: 'bottom taskbar' + (ta.value.trim() ? ' active' : '') }, h('div', { class: 'field task-field' }, ta, send), chips);
  refs.task = { ta, send, chips, bar };
  ta.addEventListener('input', () => {
    grow(ta);
    localStorage.setItem(DRAFT, ta.value);
    send.disabled = !ta.value.trim() || refs.task.busy;
    renderTaskChips();
  });
  ta.addEventListener('focus', () => bar.classList.add('active'));
  ta.addEventListener('blur', () => setTimeout(() => {
    if (!ta.value.trim() && !refs.task.picking && document.activeElement !== ta) bar.classList.remove('active');
  }, 200));
  ta.addEventListener('keydown', (e) => {
    if (e.key === 'Enter' && !e.shiftKey && (e.metaKey || e.ctrlKey || matchMedia('(pointer: fine)').matches)) {
      e.preventDefault();
      submitTask();
    }
  });
  send.addEventListener('click', submitTask);
  requestAnimationFrame(() => grow(ta));
  return bar;
}

function taskConn() {
  const all = conns().filter((c) => c.status !== 'unpaired');
  return all.find((c) => c.m.id === S.task.mid) || all.find((c) => c.status === 'connected') || all[0] || null;
}

function taskPrefs(c) {
  if (!S.task.by) S.task.by = {};
  return (S.task.by[c.m.id] = S.task.by[c.m.id] || {});
}

function saveTask() {
  localStorage.setItem('irie.task', JSON.stringify(S.task));
}

/** The @ and + words typed in the first line win over the chips; the chips show them. */
function typedWords(text) {
  const first = String(text || '').split('\n')[0];
  const repo = first.match(/(?:^|\s)@(\S+)/);
  const prof = first.match(/(?:^|\s)\+(\S+)/);
  return { repo: repo && repo[1], profile: prof && prof[1] };
}

function renderTaskChips() {
  const t = refs.task;
  if (!t) return;
  const c = taskConn();
  const all = conns();
  const typed = typedWords(t.ta.value);
  const prefs = c ? taskPrefs(c) : {};
  const key = JSON.stringify([c && c.m.id, c && c.m.name, all.length, typed, prefs]);
  if (t.key === key) return;
  t.key = key;
  const chip = (cls, sym, text, set, label, run) => h('button', { class: 'tchip ' + cls + (set ? ' set' : ''), 'aria-label': label, onclick: run },
    sym ? h('span', { class: 'sym', text: sym }) : null, h('span', { class: 'tc-text', text }), h('span', { class: 'caret', html: ICON.caret }));
  const kids = [];
  if (all.length > 1 && c) kids.push(chip('mach', '', c.m.name, true, `Machine: ${c.m.name}`, pickMachine));
  const repo = typed.repo || prefs.repo || '';
  kids.push(chip('repo', '@', typed.repo ? typed.repo : repo ? L.base(repo) : 'No repo', !!repo, `Where it runs: ${repo || 'not in a repo'}`, pickRepo));
  const prof = typed.profile || prefs.profile || '';
  kids.push(chip('prof', '+', prof || 'Default agent', !!prof, `Agent: ${prof || 'the default'}`, pickProfile));
  t.chips.replaceChildren(...kids);
}

/** A list to pick from in a sheet, with a filter when it is long. */
function picker(title, load, current, choose) {
  const t = refs.task;
  t.picking = true;
  const list = h('div', { class: 'pick-list', role: 'listbox', 'aria-label': title }, h('div', { class: 'hint reading' }, h('span', { class: 'ic', html: ICON.ring }), 'Loading…'));
  const filter = h('input', { class: 'text-in filter', type: 'search', placeholder: 'Filter', hidden: true, autocomplete: 'off', 'aria-label': 'Filter' });
  const s = sheet({
    title,
    content: h('div', { class: 'picker' }, filter, list),
    onClose: () => {
      t.picking = false;
      if (!t.ta.value.trim() && document.activeElement !== t.ta) t.bar.classList.remove('active');
    },
  });
  let options = [];
  const draw = () => {
    const q = filter.value.trim().toLowerCase();
    const shown = options.filter((o) => !q || (o.label + ' ' + (o.note || '')).toLowerCase().includes(q));
    list.replaceChildren(...shown.map((o) => h('button', {
      class: 'pick' + (o.value === current ? ' on' : ''), role: 'option', 'aria-selected': String(o.value === current),
      onclick: () => {
        s.close();
        choose(o.value);
        renderTaskChips();
        t.ta.focus({ preventScroll: true });
      },
    }, h('span', { class: 'pick-label', text: o.label }), o.note ? h('span', { class: 'pick-note', text: o.note }) : null)));
    if (!shown.length) list.append(h('div', { class: 'hint', text: 'Nothing matches.' }));
  };
  filter.addEventListener('input', draw);
  load().then((opts) => {
    options = opts;
    filter.hidden = opts.length < 9;
    draw();
  }).catch((e) => list.replaceChildren(h('div', { class: 'problem', text: e.message })));
}

function pickMachine() {
  const cur = taskConn();
  picker('Run it on', async () => conns().filter((c) => c.status !== 'unpaired').map((c) => ({ value: c.m.id, label: c.m.name, note: statusWords(c) })),
    cur && cur.m.id, (v) => {
      S.task.mid = v;
      saveTask();
    });
}

function pickRepo() {
  const c = taskConn();
  if (!c) return;
  const prefs = taskPrefs(c);
  picker('Where it runs', async () => {
    const r = await call(c.m, 'complete', { text: '@' }, { timeout: 8000 });
    const items = (r.items || []).map((i) => i.replace(/^@/, ''));
    return [{ value: '', label: 'Not in a repo', note: 'A folder of its own' }, ...items.map((name) => ({ value: name, label: '@' + name, note: 'Its own worktree' }))];
  }, prefs.repo || '', (v) => {
    prefs.repo = v;
    saveTask();
  });
}

function pickProfile() {
  const c = taskConn();
  if (!c) return;
  const prefs = taskPrefs(c);
  picker('Agent', async () => {
    const r = await call(c.m, 'complete', { text: '+' }, { timeout: 8000 });
    const notes = r.notes || [];
    return [{ value: '', label: 'The default', note: 'As the machine’s config says' },
      ...(r.items || []).map((i, k) => ({ value: i.replace(/^\+/, ''), label: i, note: notes[k] || '' }))];
  }, prefs.profile || '', (v) => {
    prefs.profile = v;
    saveTask();
  });
}

async function submitTask() {
  const t = refs.task;
  const text = t.ta.value.trim();
  const c = taskConn();
  if (!text || t.busy) return;
  if (!c) return toast('No machine to run it on.', { error: true });
  const prefs = taskPrefs(c);
  const args = { text };
  if (prefs.repo) args.repo = prefs.repo;
  if (prefs.profile) args.profile = prefs.profile;
  t.busy = true;
  t.send.disabled = true;
  t.send.classList.add('busy');
  try {
    // A task in a repo may make a worktree first, so this can take a while.
    const r = await call(c.m, 'input', args, { timeout: 120000 });
    if (t.ta.value.trim() === text) {
      t.ta.value = '';
      localStorage.removeItem(DRAFT);
      grow(t.ta);
      renderTaskChips();
    }
    if (r.did === 'asked' && r.choice && r.op) return afterHistory(c, r.op, r);
    const s = r.session;
    if (r.did === 'spawned' && s) {
      c.sessions.set(s.id, s);
      invalidate();
      if (r.show) openSession(c.m.id, s.id);
      else toast(r.text || `Started “${s.name}”`, { action: { label: 'Open', run: () => openSession(c.m.id, s.id) } });
    } else if (s && r.show) {
      openSession(c.m.id, s.id);
    } else if (r.text) {
      toast(r.text, { ms: r.did === 'answered' ? 9000 : 4500 });
    }
  } catch (e) {
    fail(c, e);
  } finally {
    t.busy = false;
    t.send.classList.remove('busy');
    t.send.disabled = !t.ta.value.trim();
  }
}

// ---- The machines sheet -----------------------------------------------------

function openMachines() {
  // Only the cards are drawn again; the form keeps its text and the keyboard.
  const cards = h('div', { class: 'mcards' });
  const notes = pushPanel();
  const body = h('div', { class: 'machines' }, cards, notes.el, addMachineForm());
  const draw = () => {
    notes.draw();
    const key = JSON.stringify(conns().map((c) => [c.m.id, c.m.name, c.status, c.info, c.version, c.header && c.header.plan, c.confirming]));
    if (cards._key === key) return;
    cards._key = key;
    cards.replaceChildren(...conns().map(machineCard));
  };
  draw();
  const s = sheet({ title: 'Machines', content: body, className: 'tall', onClose: () => (refs.machinesSheet = null) });
  refs.machinesSheet = { render: draw, sheet: s };
}

const OS = { darwin: 'macOS', linux: 'Linux', windows: 'Windows' };

function machineCard(c) {
  const i = c.info || {};
  // The release, without the build's hash after the +.
  const version = String(i.version || c.version || '').split('+')[0];
  const meta = [version ? 'Irie ' + version : '', OS[i.os] || i.os || '', host(c.m.url)].filter(Boolean).join(' · ');
  const plan = c.header && c.header.plan;
  const kids = [
    h('div', { class: 'mc-head' }, h('span', { class: 'dot ' + c.status, 'aria-hidden': 'true' }), h('span', { class: 'mc-name', text: c.m.name }),
      h('span', { class: 'mc-status ' + c.status, text: statusWords(c) })),
    h('div', { class: 'mc-meta', text: meta }),
  ];
  if (plan && (plan.five_hour || plan.seven_day)) {
    kids.push(h('div', { class: 'meters', 'aria-label': 'Claude plan use' },
      plan.five_hour ? meter('5 hours', plan.five_hour) : null, plan.seven_day ? meter('Week', plan.seven_day) : null));
  }
  if (c.status === 'unpaired') {
    kids.push(h('div', { class: 'banner loud' }, h('span', { text: 'It no longer knows this phone.' }),
      h('button', { class: 'btn small primary', text: 'Pair again', onclick: () => pairAgain(c) })));
  }
  if (c.confirming) {
    kids.push(h('div', { class: 'confirm' },
      h('div', { class: 'confirm-text', text: `Forget ${c.m.name}? This phone signs out of it. A new code pairs them again.` }),
      h('div', { class: 'confirm-row' },
        h('button', { class: 'btn', text: 'Cancel', onclick: () => { c.confirming = false; refs.machinesSheet && refs.machinesSheet.render(); } }),
        h('button', { class: 'btn danger', text: 'Forget', onclick: () => forgetMachine(c) }))));
  } else {
    kids.push(h('div', { class: 'mc-actions' },
      c.status === 'offline' ? h('button', { class: 'btn small', text: 'Retry', onclick: () => c.stream && c.stream.kick() }) : null,
      h('button', { class: 'btn small ghost-danger', text: 'Forget', onclick: () => { c.confirming = true; refs.machinesSheet && refs.machinesSheet.render(); } })));
  }
  return h('div', { class: 'mcard' }, kids);
}

function meter(label, w) {
  const used = Math.max(0, Math.min(100, w.used || 0));
  const fill = h('span', { class: 'm-fill' + (used >= 95 ? ' hot' : used >= 80 ? ' warm' : '') });
  fill.style.width = used + '%';
  const reset = L.when(w.resets_at);
  return h('div', { class: 'meter' },
    h('span', { class: 'm-label', text: label }),
    h('span', { class: 'm-bar', role: 'meter', 'aria-valuemin': '0', 'aria-valuemax': '100', 'aria-valuenow': String(Math.round(used)), 'aria-label': `${label}: ${Math.round(used)}% used` }, fill),
    h('span', { class: 'm-val', text: Math.round(used) + '%' }),
    reset ? h('span', { class: 'm-reset', text: 'resets ' + L.clock(reset) }) : null);
}

async function forgetMachine(c) {
  try {
    await forget(c.m);
  } catch (e) {
    if (e.status !== 401) toast(`Forgot ${c.m.name} here. ${e.message}`, { error: true });
  }
  c.stop();
  S.conns.delete(c.m.id);
  S.machines = S.machines.filter((m) => m.id !== c.m.id);
  saveMachines(S.machines);
  forgetSent(c.m.id);
  if (S.view && S.view.conn === c) back();
  if (!S.machines.length) {
    closeSheet();
    show();
    return;
  }
  refs.machinesSheet && refs.machinesSheet.render();
  invalidate();
}

function addMachineForm() {
  const here = S.machines.some((m) => m.url === location.origin) ? '' : location.host;
  const addr = h('input', { class: 'text-in', type: 'text', placeholder: '10.0.0.7 or studio.local', value: here, autocapitalize: 'off', autocorrect: 'off', spellcheck: 'false', inputmode: 'url', 'aria-label': 'Address' });
  const code = h('input', { class: 'text-in code-small', type: 'text', inputmode: 'numeric', autocomplete: 'one-time-code', pattern: '[0-9]*', maxlength: '6', placeholder: 'Code', 'aria-label': 'Pairing code' });
  const problem = h('div', { class: 'problem', role: 'alert' });
  const go = h('button', { class: 'btn primary', type: 'submit', text: 'Add' });
  const form = h('form', { class: 'add-machine' },
    h('h3', { class: 'group-title', text: 'Add a machine' }),
    addr,
    h('div', { class: 'add-row' }, code, go),
    h('p', { class: 'hint' }, 'On that machine, run ', h('code', { text: 'irie remote pair' }), '. A machine new to this phone needs its setup page first: port 7386, over http.'),
    problem);
  code.addEventListener('input', () => {
    code.value = code.value.replace(/\D/g, '').slice(0, 6);
    problem.textContent = '';
  });
  form.addEventListener('submit', async (e) => {
    e.preventDefault();
    const url = normalize(addr.value);
    const c = code.value.replace(/\D/g, '');
    if (!url) return (problem.textContent = 'That doesn’t look like an address.');
    if (c.length !== 6) return (problem.textContent = 'The code has 6 digits.');
    go.disabled = true;
    problem.textContent = '';
    try {
      const m = remember(url, await pair(url, c, DEVICE));
      addr.value = '';
      code.value = '';
      toast(`Paired with ${m.name}.`);
      invalidate();
    } catch (err) {
      problem.replaceChildren(err.message);
      if (err.code === 'offline' && url !== location.origin) {
        const setup = url.replace(/^https:/, 'http:').replace(/:\d+$/, '') + ':7386/';
        problem.append(' If it is, set it up for this phone first: ', h('a', { href: setup, target: '_blank', rel: 'noopener', text: setup }));
      }
    } finally {
      go.disabled = false;
    }
  });
  return form;
}

// ---- Notifications ------------------------------------------------------------
// Web Push, from the Home Screen app only (iOS 16.4 and later). Each machine
// gets this phone's subscription and key once; what was sent is kept, so a
// start sends again only when the subscription changed, as each send makes
// the machine push a first notification.

const pushState = { busy: false, problem: '' };
const SENT = 'irie.pushSent';

const pushable = () => Push.supported() && STANDALONE;
const pushReady = () => pushable() && Push.prefs().on && Notification.permission === 'granted';
const sentKey = (sub, done) => sub.endpoint + '|' + (done ? 1 : 0);

function forgetSent(id) {
  const sent = read(SENT, {});
  if (id) delete sent[id];
  localStorage.setItem(SENT, JSON.stringify(id ? sent : {}));
}

function redrawPush() {
  if (refs.machinesSheet) refs.machinesSheet.render();
}

function pushPanel() {
  const box = h('div', { class: 'push-panel' });
  const draw = () => {
    const p = Push.prefs();
    const on = pushReady();
    const key = JSON.stringify([pushable(), on, p.done, pushState, conns().map((c) => [c.m.name, c.push || ''])]);
    if (box._key === key) return;
    box._key = key;
    box.replaceChildren(...pushRows(on, p));
  };
  return { el: box, draw };
}

function pushRows(on, p) {
  if (!pushable()) {
    if (!IOS || STANDALONE) return [];
    return [h('h3', { class: 'group-title', text: 'Notifications' }),
      h('p', { class: 'hint pad', text: 'Notifications work once Irie is on your Home Screen: open it from there to turn them on.' })];
  }
  const toggle = (checked, label, run) => h('button', {
    class: 'switch', role: 'switch', 'aria-checked': String(checked), 'aria-label': label, disabled: pushState.busy, onclick: run,
  });
  const row = (label, note, sw) => h('div', { class: 'set-row' },
    h('div', { class: 'set-text' }, h('div', { class: 'set-label', text: label }), note ? h('div', { class: 'set-note', text: note }) : null), sw);
  const out = [
    h('h3', { class: 'group-title', text: 'Notifications' }),
    h('div', { class: 'set-group' },
      row('Notify me', 'When a session needs you, fails or stalls', toggle(on, 'Notify me', togglePush)),
      on ? row('Also when a task finishes', 'Its turn is over, to verify', toggle(p.done, 'Also when a task finishes', toggleDone)) : null),
  ];
  if (on) out.push(h('div', { class: 'push-lines' }, conns().map(pushLine)));
  if (pushState.problem) out.push(h('div', { class: 'problem pad', role: 'alert', text: pushState.problem }));
  return out;
}

function pushLine(c) {
  const st = c.push || '';
  const ok = st === 'on';
  const busy = st === 'sending';
  return h('div', { class: 'push-line' + (ok ? ' ok' : !busy && st ? ' bad' : '') },
    h('span', { class: 'dot ' + (ok ? 'connected' : busy ? 'connecting' : st ? 'unpaired' : 'offline'), 'aria-hidden': 'true' }),
    h('span', { class: 'pl-name', text: c.m.name }),
    h('span', { class: 'pl-state', text: ok ? 'On' : busy ? 'Turning on…' : st || 'Not yet' }),
    !ok && !busy ? h('button', { class: 'linkish', text: 'Try again', onclick: () => syncPush([c], true) }) : null);
}

/** The switch: it asks for permission in the tap itself, as iOS wants. */
function togglePush() {
  if (pushReady()) return pushOff();
  let asked;
  try {
    asked = Notification.requestPermission();
  } catch {
    asked = Promise.resolve(Notification.permission);
  }
  Promise.resolve(asked).then(pushOn, (e) => {
    pushState.problem = e.message || String(e);
    redrawPush();
  });
}

async function pushOn(perm) {
  if (perm !== 'granted') {
    pushState.problem = perm === 'denied'
      ? 'Notifications are off for Irie. Turn them on in Settings → Notifications → Irie.'
      : 'Irie can’t notify you until you allow it.';
    return redrawPush();
  }
  const p = Push.prefs();
  p.on = true;
  Push.savePrefs(p);
  pushState.problem = '';
  await syncPush(conns(), true);
}

/** Sends the subscription to these machines; all of them, or only those it changed for. */
async function syncPush(list, always) {
  list = list.filter((c) => c && c.status !== 'unpaired');
  if (!list.length) return;
  pushState.busy = true;
  redrawPush();
  try {
    const { sub, k } = await Push.subscription();
    const done = Push.prefs().done;
    const key = sentKey(sub, done);
    const sent = read(SENT, {});
    await Promise.all(list.map(async (c) => {
      if (!always && sent[c.m.id] === key) {
        c.push = 'on';
        return;
      }
      if (c.info && !('push' in c.info)) {
        c.push = 'Its Irie can’t send notifications yet';
        return;
      }
      c.push = 'sending';
      redrawPush();
      try {
        await Push.tell(c.m, sub, k, done);
        c.push = 'on';
        sent[c.m.id] = key;
      } catch (e) {
        c.push = e.status === 404 || e.status === 405 ? 'Its Irie can’t send notifications yet' : e.message;
        if (e.status === 401) unpaired(c, e.message);
      }
      redrawPush();
    }));
    localStorage.setItem(SENT, JSON.stringify(sent));
  } catch (e) {
    pushState.problem = e.message || String(e);
  } finally {
    pushState.busy = false;
    redrawPush();
  }
}

async function pushOff() {
  const p = Push.prefs();
  p.on = false;
  Push.savePrefs(p);
  pushState.busy = true;
  pushState.problem = '';
  redrawPush();
  await Promise.all(conns().map(async (c) => {
    try {
      await Push.untell(c.m);
    } catch {
      // A machine that can't be reached drops this phone when its push fails.
    }
    c.push = '';
  }));
  try {
    await Push.unsubscribe();
  } catch {
    // Nothing to end.
  }
  forgetSent();
  pushState.busy = false;
  redrawPush();
}

function toggleDone() {
  const p = Push.prefs();
  p.done = !p.done;
  Push.savePrefs(p);
  syncPush(conns(), true);
}

/** At start: keeps the machines' notifications going as this phone's subscription changes. */
function resumePush() {
  if (!pushable() || !Push.prefs().on) return;
  if (Notification.permission !== 'granted') {
    // Turned off in Settings: so is the switch.
    Push.savePrefs({ ...Push.prefs(), on: false });
    return;
  }
  syncPush(conns(), false);
}

// ---- The session screen and the hash ---------------------------------------

const ctx = {
  back: () => back(),
  changed,
  unpaired,
  fail,
  reopen,
  sessionActions,
  op: (c, name, args) => op(c, name, args),
};

function openSession(mid, sid) {
  const hash = `#/m/${encodeURIComponent(mid)}/s/${encodeURIComponent(sid)}`;
  if (location.hash === hash) return;
  S.pushed = true;
  location.hash = hash;
}

function back() {
  if (S.pushed) history.back();
  else location.replace('#/');
}

function route() {
  if (S.screen !== 'home') return;
  const m = location.hash.match(/^#\/m\/([^/]+)\/s\/([^/?#]+)/);
  if (!m) {
    S.pushed = false;
    closeView();
    return;
  }
  const c = S.conns.get(decodeURIComponent(m[1]));
  const sid = decodeURIComponent(m[2]);
  if (!c) {
    location.replace('#/');
    return;
  }
  if (S.view && S.view.conn === c && S.view.sid === sid) return;
  closeView(true);
  closeSheet();
  if (S.openRow) setOpen(S.openRow, false);
  const v = new SessionView(ctx, c, sid);
  S.view = v;
  app.append(v.el);
  trackBottom();
  refs.home.setAttribute('aria-hidden', 'true');
  requestAnimationFrame(() => requestAnimationFrame(() => {
    v.el.classList.add('in');
    refs.home.classList.add('covered');
  }));
}

function closeView(instant) {
  const v = S.view;
  if (!v) return;
  S.view = null;
  v.destroy();
  if (refs.home) {
    refs.home.classList.remove('covered');
    refs.home.removeAttribute('aria-hidden');
  }
  v.el.classList.remove('in');
  trackBottom();
  const reduced = matchMedia('(prefers-reduced-motion: reduce)').matches;
  setTimeout(() => v.el.remove(), instant || reduced ? 0 : 340);
}

window.addEventListener('hashchange', route);

// ---- New versions --------------------------------------------------------------
// The phone picks up a new install on its own: a new service worker, or a
// new build of the machine that served the app, reloads it once nobody is
// typing, answering or picking. The shell comes from the network first.

let reloadWanted = false;
let build = '';

function idle() {
  const a = document.activeElement;
  if (a && /^(INPUT|TEXTAREA|SELECT)$/.test(a.tagName)) return false;
  if (openSheet()) return false;
  if (refs.task && (refs.task.busy || refs.task.picking)) return false;
  if (S.view && (S.view.sending || S.view.busyKey || S.view.input.value.trim())) return false;
  return true;
}

function reloadSoon() {
  reloadWanted = true;
  tryReload();
}

function tryReload() {
  if (reloadWanted && (document.visibilityState === 'hidden' || idle())) location.reload();
}

async function checkBuild() {
  try {
    const i = await info(location.origin);
    if (i && i.version) {
      if (!build) build = i.version;
      else if (i.version !== build) reloadSoon();
    }
  } catch {
    // Asleep or offline: next time.
  }
  if ('serviceWorker' in navigator) {
    navigator.serviceWorker.getRegistration().then((r) => r && r.update()).catch(() => {});
  }
}

/** Opens what a notification is about: a session, or the list. */
function openHash(url) {
  if (typeof url !== 'string') return;
  if (!/^#\/m\/[^/]+\/s\/[^/?#]+$/.test(url)) {
    if (S.view) location.replace('#/');
    return;
  }
  if (location.hash === url) return;
  if (S.view || S.screen !== 'home') location.replace(url);
  else {
    S.pushed = true;
    location.hash = url;
  }
}

if ('serviceWorker' in navigator) {
  let had = !!navigator.serviceWorker.controller;
  navigator.serviceWorker.addEventListener('controllerchange', () => {
    // The first worker taking the page is not news; a later one is.
    if (had) reloadSoon();
    had = true;
  });
  navigator.serviceWorker.addEventListener('message', (e) => openHash(e.data && e.data.open));
}
document.addEventListener('focusout', () => setTimeout(tryReload, 400));
setInterval(tryReload, 5000);
setInterval(() => {
  if (document.visibilityState === 'visible') checkBuild();
}, 5 * 60e3);

// ---- Toasts' place ------------------------------------------------------------
// Toasts float just above the bottom bar on show, whatever its height.

const bottomRO = 'ResizeObserver' in window ? new ResizeObserver(() => setBottom()) : null;

function bottomBar() {
  if (S.view) return S.view.bottomEl;
  return S.screen === 'home' && refs.task ? refs.task.bar : null;
}

function setBottom() {
  const el = bottomBar();
  root.style.setProperty('--bottom-h', (el ? el.getBoundingClientRect().height : 0) + 'px');
}

function trackBottom() {
  if (bottomRO) {
    bottomRO.disconnect();
    const el = bottomBar();
    if (el) bottomRO.observe(el);
  }
  setBottom();
}

// ---- Coming back --------------------------------------------------------------
// iOS ends streams in the background, often without a word: a stream that
// went quiet while the app was away is taken for dead.

function kickAll() {
  for (const c of conns()) {
    if (c.status === 'unpaired') continue;
    if (c.stream) c.stream.kick();
    if (!c.info) c.fetchInfo();
  }
  if (S.view && S.view.conn.status !== 'unpaired') S.view.stream.kick();
}

document.addEventListener('visibilitychange', () => {
  if (document.visibilityState === 'hidden') {
    S.hiddenAt = Date.now();
    tryReload();
  } else if (Date.now() - S.hiddenAt > 2500) {
    kickAll();
    checkBuild();
  }
});
window.addEventListener('online', kickAll);
window.addEventListener('pageshow', (e) => {
  if (e.persisted) kickAll();
});
// Ages move on.
setInterval(() => {
  if (document.visibilityState === 'visible') invalidate();
}, 30000);

// ---- Start ------------------------------------------------------------------

app.append(h('div', { id: 'toasts', class: 'toasts', 'aria-live': 'polite' }));
show();
checkBuild();
resumePush();

if ('serviceWorker' in navigator && window.isSecureContext && !params.has('nosw')) {
  navigator.serviceWorker.register('sw.js').catch(() => {});
}
