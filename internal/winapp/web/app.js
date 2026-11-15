// Irie for Windows: the window's page (docs/WINDOWS.md). A sidebar of
// session cards over a task bar, and the slot's terminal beside it, as the
// Mac app draws them (docs/MAC.md). The core owns every rule
// (docs/CORE.md): the page sends what the person does to the ops it names
// and draws what the core says. Its looks come from the phone app's
// look.js, so a row reads the same on every screen.

import { h, ICON } from './shared/ui.js';
import * as L from './shared/look.js';
import { esc } from './shared/md.js';
import { S, apply, ordered, matches, listSections, repoSections, loudIn, countIn, counts, walk, needsYou } from './js/store.js';
import { call, Stream, on, hello, host, shown, ApiError } from './js/net.js';
import { SlotTerminal } from './js/terminal.js';
import { menu, dialog, ask, confirm, picker, notice, closeAll, isOpen } from './js/menu.js';
import { renderSetup } from './js/setup.js';
import { openSettings } from './js/settings.js';

/** Puts kids in el in place of what it holds, leaving out null and false, as h() does. */
const fill = (el, ...kids) => el.replaceChildren(...kids.flat(Infinity).filter((k) => k != null && k !== false));

// ---- What the page keeps ----------------------------------------------------

const store = {
  get(k, d) {
    try {
      const v = localStorage.getItem('irie.' + k);
      return v == null ? d : JSON.parse(v);
    } catch {
      return d;
    }
  },
  set(k, v) {
    try {
      localStorage.setItem('irie.' + k, JSON.stringify(v));
    } catch {}
  },
};

const U = {
  link: { state: 'looking', text: '' },
  setup: null,
  features: new Set(),
  version: '',
  selected: '',
  outsideSelected: '',
  view: store.get('view', 'list'), // list or repo
  folded: store.get('folded', {}),
  find: '',
  space: '',
  side: store.get('side', 300),
  task: store.get('task', { repo: '', profile: '' }),
  history: store.get('taskHistory', []),
  renaming: '',
  lastLoud: new Map(),
  focusedWindow: true,
};

// ---- The window's parts -------------------------------------------------------

const refs = {};

function build() {
  const app = document.getElementById('app');
  refs.counts = h('div', { class: 'counts', role: 'toolbar', 'aria-label': 'Counts' });
  refs.brand = h('div', { class: 'brand', html: L.sun('sun') + `<span class="brand-name">Irie</span>` });
  refs.newBtn = h('button', { class: 'icon-btn', title: 'New task (Ctrl+Shift+N)', 'aria-label': 'New task', html: ICON.add, onclick: () => focusTask() });
  refs.moreBtn = h('button', { class: 'icon-btn', title: 'Menu', 'aria-label': 'Menu', html: ICON.more, onclick: (e) => appMenu(e.currentTarget) });
  refs.find = h('input', { class: 'find', type: 'search', placeholder: 'Find a session', 'aria-label': 'Find a session', spellcheck: 'false' });
  refs.viewSwitch = h('div', { class: 'seg', role: 'tablist', 'aria-label': 'View' },
    h('button', { role: 'tab', 'data-v': 'list', text: 'List', onclick: () => setView('list') }),
    h('button', { role: 'tab', 'data-v': 'repo', text: 'Repo', onclick: () => setView('repo') }));
  refs.spaces = h('div', { class: 'spaces', role: 'tablist', 'aria-label': 'Spaces', hidden: true });
  refs.list = h('div', { class: 'list', role: 'listbox', 'aria-label': 'Sessions', tabindex: '0' });
  refs.ready = h('div', { class: 'ready-note', hidden: true });
  refs.details = h('section', { class: 'details', 'aria-label': 'Details', hidden: true });
  refs.taskInput = h('textarea', { class: 'task-in', rows: '2', placeholder: 'Type a task — @repo, +agent, or a :command', 'aria-label': 'Task', spellcheck: 'true' });
  refs.taskSend = h('button', { class: 'send', 'aria-label': 'Start the task', title: 'Start (Enter)', html: ICON.up, disabled: true });
  refs.taskChips = h('div', { class: 'task-chips' });
  refs.taskbar = h('footer', { class: 'taskbar' }, h('div', { class: 'task-field' }, refs.taskInput, refs.taskSend), refs.taskChips);
  refs.side = h('aside', { class: 'side', style: `width:${U.side}px` },
    h('header', { class: 'side-head' }, refs.brand, refs.counts, h('div', { class: 'grow' }), refs.newBtn, refs.moreBtn),
    h('div', { class: 'side-tools' }, refs.find, refs.viewSwitch),
    refs.spaces, refs.ready, refs.list, refs.details, refs.taskbar);
  refs.divider = h('div', { class: 'divider', role: 'separator', 'aria-orientation': 'vertical', 'aria-label': 'Sidebar width', tabindex: '-1' });
  refs.headMark = h('span', { class: 'mark' });
  refs.headName = h('div', { class: 'head-name' });
  refs.headLine = h('div', { class: 'head-line' });
  refs.headActs = h('div', { class: 'head-acts' });
  refs.head = h('header', { class: 'main-head' }, refs.headMark, h('div', { class: 'head-text' }, refs.headName, refs.headLine), refs.headActs);
  refs.termEl = h('div', { class: 'term' });
  refs.termNote = h('div', { class: 'term-note', hidden: true });
  refs.termCover = h('div', { class: 'term-cover', hidden: true });
  refs.frame = h('div', { class: 'frame' }, refs.termEl, refs.termCover, refs.termNote);
  refs.main = h('main', { class: 'main' }, refs.head, refs.frame);
  refs.overlay = h('div', { class: 'overlay', hidden: true });
  refs.welcome = h('div', { class: 'welcome', hidden: true });
  refs.style = h('style');
  document.head.append(refs.style);
  fill(app, h('div', { class: 'shell' }, refs.side, refs.divider, refs.main), refs.overlay, refs.welcome);
  wire();
}

// ---- Looks --------------------------------------------------------------------

let paletteKey = '';

function paint() {
  const key = JSON.stringify(S.header.theme || null);
  if (key === paletteKey && refs.style.textContent) return;
  paletteKey = key;
  if (S.header.theme) L.usePalette(S.header.theme);
  refs.style.textContent = [...L.paletteRules(), ...L.agentRules()].join('\n');
  lookWindow();
}

const dark = matchMedia('(prefers-color-scheme: dark)');
dark.addEventListener('change', () => lookWindow());

/** The title bar takes the sidebar's colour, as the Mac's does. */
function lookWindow() {
  const set = dark.matches ? L.PALETTE.dark : L.PALETTE.light;
  const hex = (n) => '#' + n.toString(16).padStart(6, '0');
  host.look({ dark: dark.matches, caption: hex(set.sidebar), text: hex(set.ink), border: hex(set.edge) });
}

// ---- The link to Irie inside WSL ------------------------------------------------

on('link', (m) => {
  const was = U.link.state;
  U.link = m;
  if (m.state === 'ready' && was !== 'ready') connected();
  if (m.state !== 'ready' && was === 'ready') disconnected();
  drawLink();
});

on('setup', (m) => {
  U.setup = m;
  drawLink();
});

function drawLink() {
  const st = U.link.state;
  const setup = st === 'setup' || (U.setup && !U.setup.ready && st !== 'ready' && st !== 'connecting');
  refs.welcome.hidden = !setup;
  if (setup) renderSetup(refs.welcome, U.setup, U.link);
  document.body.classList.toggle('offline', st !== 'ready');
  const cover = st === 'ready' || setup ? null : coverFor(st);
  refs.overlay.hidden = !cover;
  if (cover) fill(refs.overlay, cover);
  title();
}

function coverFor(st) {
  const text = U.link.text || '';
  const card = (...kids) => h('div', { class: 'cover-card' }, h('div', { class: 'cover-sun', html: L.sun('sun big') }), ...kids);
  switch (st) {
    case 'looking':
    case 'connecting':
      return card(h('div', { class: 'cover-title' }, h('span', { class: 'spin-ring', html: ICON.ring }), text || 'Connecting to Irie…'),
        h('p', { class: 'cover-sub', text: 'Linux may take a moment to start the first time.' }));
    case 'old':
      return card(h('div', { class: 'cover-title', text: 'Irie inside Linux needs an update' }), h('p', { class: 'cover-sub', text }),
        h('div', { class: 'cover-acts' }, h('button', { class: 'btn primary', text: `Update Irie in ${U.link.distro || 'Linux'}`, onclick: () => host.fix('install') }),
          h('button', { class: 'btn', text: 'Try again', onclick: () => host.retry() })),
        U.setup && U.setup.fixing === 'install' ? h('p', { class: 'cover-sub', text: 'Updating in a window of its own… It may ask for your Linux password.' }) : null);
    default:
      return card(h('div', { class: 'cover-title', text: 'Irie isn’t answering' }), h('p', { class: 'cover-sub', text: text || 'The connection to Irie inside Linux failed.' }),
        h('div', { class: 'cover-acts' }, h('button', { class: 'btn primary', text: 'Try again', onclick: () => host.retry() })));
  }
}

// ---- The subscription ---------------------------------------------------------------

let stream = null;

/** The palette, told to the core as the Mac app tells it, so Irie's status line in the terminal and Claude Code's panels match the cards. */
async function sayTheme() {
  let name = store.get('theme', '');
  if (!name) {
    try {
      const r = await call('config', {}, { timeout: 8000 });
      name = (r.config && r.config.ui && r.config.ui.theme) || 'rosewater';
    } catch {
      name = 'rosewater';
    }
  }
  try {
    await call('theme', { theme: name }, { timeout: 8000 });
  } catch {}
}

function connected() {
  coreHello();
  sayTheme();
  if (!stream) {
    stream = new Stream({ view: 'app' }, {
      frame: (f) => {
        const what = apply(f);
        if (what === 'rows') notifyChanges();
        if (what === 'header') paint();
        if (f.t === 'snapshot') {
          paint();
          const shown = [...S.rows.values()].find((s) => s.shown);
          if (shown && !U.selected) U.selected = shown.id;
        }
        if (f.t === 'card') return;
        schedule();
      },
      state: (st) => {
        document.body.classList.toggle('stale', st !== 'connected');
      },
      refused: (e) => notice(e.message, { error: true }),
    });
  }
  stream.start();
  term.attach();
  readyCheck();
}

function disconnected() {
  document.body.classList.add('stale');
}

/** What the core serves: the features its hello names. */
async function coreHello() {
  try {
    const r = await call('hello', { client: 'windows', api: 1, version: U.link.app || '' }, { timeout: 8000 });
    U.features = new Set(r.features || []);
    U.version = r.version || '';
  } catch {}
}

/** What the computer still lacks of what Irie needs (`ready`), and its fix. */
async function readyCheck() {
  try {
    const r = await call('ready', {}, { timeout: 15000 });
    const missing = (r.items || []).filter((it) => it.blocks && it.status !== 'ok');
    refs.ready.hidden = !missing.length;
    if (missing.length) {
      fill(refs.ready, 
        h('div', { class: 'ready-text' }, h('b', { text: 'Irie still needs ' + missing.map((it) => it.name).join(', ') + '.' }), ' ',
          missing[0].problem || missing[0].for || ''),
        h('button', { class: 'btn small primary', text: 'Set up', onclick: () => runTool('ready') }));
    }
  } catch {
    refs.ready.hidden = true;
  }
}

/** A session in a reply: the core sends the row; an id will do too. */
const idOf = (s) => (s && typeof s === 'object' ? s.id : s) || '';

async function runTool(name) {
  try {
    const r = await call('tool', { name }, { timeout: 30000 });
    if (idOf(r && r.session)) select(idOf(r.session), true);
  } catch (e) {
    notice(e.message, { error: true });
  }
}

// ---- Drawing, a frame at a time ------------------------------------------------------

let pending = false;

function schedule() {
  if (pending) return;
  pending = true;
  requestAnimationFrame(() => {
    pending = false;
    render();
  });
}

function folded(key, def = false) {
  if (U.find) return false;
  return key in U.folded ? U.folded[key] : def;
}

function fold(key, def) {
  U.folded[key] = !folded(key, def);
  store.set('folded', U.folded);
  schedule();
}

let sections = [];

function render() {
  paint();
  const spaces = S.header.spaces || [];
  if (U.space && !spaces.some((sp) => (sp.id || sp.name) === U.space)) U.space = '';
  // Another machine's rows show in every space; the rest in their own.
  const rows = ordered().filter((s) => s.host || (s.space || '') === U.space).filter((s) => matches(s, U.find));
  sections = U.view === 'repo' ? repoSections(rows) : listSections(rows, S.header.groups || []);
  drawSpaces(spaces);
  drawList();
  if (S.loaded) requestAnimationFrame(() => shown());
  drawCounts();
  drawDetails();
  drawHead();
  title();
  for (const b of refs.viewSwitch.children) b.setAttribute('aria-selected', String(b.dataset.v === U.view));
}

function drawSpaces(spaces) {
  refs.spaces.hidden = spaces.length < 2;
  if (refs.spaces.hidden) return;
  const key = JSON.stringify([spaces, U.space]);
  if (refs.spaces._key === key) return;
  refs.spaces._key = key;
  fill(refs.spaces, ...spaces.map((sp, i) => {
    const id = i === 0 ? '' : sp.id || sp.name;
    return h('button', { role: 'tab', 'aria-selected': String(id === U.space), text: sp.name || `Space ${i + 1}`, title: `Ctrl+Alt+${i + 1}`, onclick: () => setSpace(id) });
  }));
}

function setSpace(id) {
  U.space = id;
  schedule();
}

const cards = new Map();

function drawList() {
  const now = Date.now();
  const frag = [];
  const keep = new Set();
  let any = false;
  for (const sec of sections) {
    const n = countIn(sec.items);
    if (!n && sec.key !== 'main') continue;
    if (sec.title) {
      const isFolded = folded(sec.key, sec.foldDefault);
      const loud = loudIn(sec.items);
      frag.push(groupHead(sec.key, sec.title, n, loud, isFolded, sec.foldDefault, sec.key === 'parked' ? 'park' : sec.key === 'pinned' ? 'pin' : ''));
      if (isFolded) continue;
    }
    if (n) any = true;
    place(sec.items, frag, keep, now, 0);
  }
  // Outside Irie: Claude Code's sessions that run outside it (DESIGN §4.6).
  const outside = (S.outside || []).filter((o) => !U.find || [o.name, o.cwd].join(' ').toLowerCase().includes(U.find.toLowerCase()));
  if (outside.length) {
    const isFolded = folded('outside', false);
    frag.push(groupHead('outside', 'Outside Irie', outside.length, 0, isFolded, false, ''));
    if (!isFolded) for (const o of outside) frag.push(outsideRow(o));
  }
  // Closed: what reopen can bring back.
  if (S.closed && S.closed.length && !U.find) {
    const isFolded = folded('closed', true);
    frag.push(groupHead('closed', 'Recently closed', S.closed.length, 0, isFolded, true, ''));
    if (!isFolded) for (const c of S.closed.slice(0, 12)) frag.push(closedRow(c, now));
  }
  if (!any && !outside.length) frag.push(emptyNote());
  for (const [id, el] of cards) if (!keep.has(id)) cards.delete(id);
  // Put the list in this order, moving only what moved.
  let cursor = refs.list.firstChild;
  for (const el of frag) {
    if (el === cursor) cursor = cursor.nextSibling;
    else refs.list.insertBefore(el, cursor);
  }
  while (cursor) {
    const next = cursor.nextSibling;
    cursor.remove();
    cursor = next;
  }
}

function place(items, frag, keep, now, depth) {
  for (const it of items) {
    if (it.kind === 'row') {
      keep.add(it.s.id);
      frag.push(card(it.s, now, depth));
    } else {
      const key = 'group:' + it.g.id;
      const isFolded = folded(key, false);
      frag.push(groupHead(key, it.g.name || 'Group', countIn(it.items), loudIn(it.items), isFolded, false, 'group', depth, it.g));
      if (!isFolded) place(it.items, frag, keep, now, depth + 1);
    }
  }
}

const heads = new Map();

function groupHead(key, title, n, loud, isFolded, def, drop, depth = 0, group = null) {
  let el = heads.get(key);
  if (!el) {
    el = h('button', { class: 'group-head', 'data-key': key, onclick: () => fold(key, def) });
    el.addEventListener('contextmenu', (e) => {
      e.preventDefault();
      if (group) groupMenu(e, group);
    });
    dropTarget(el, () => ({ kind: drop, key, group }));
    heads.set(key, el);
  }
  const sig = JSON.stringify([title, n, loud, isFolded, depth]);
  if (el._sig !== sig) {
    el._sig = sig;
    el.style.setProperty('--depth', depth);
    el.setAttribute('aria-expanded', String(!isFolded));
    fill(el, h('span', { class: 'chev' + (isFolded ? '' : ' open'), html: ICON.chevron }), h('span', { class: 'gh-title', text: title }),
      loud && isFolded ? h('span', { class: 'gh-loud', text: `${loud} need${loud === 1 ? 's' : ''} you` }) : null,
      h('span', { class: 'gh-count', text: String(n) }));
  }
  return el;
}

/** The second line: what it needs, what it does, or how it ended. */
function statusLine(s, lk) {
  switch (lk) {
    case 'starting': return 'Starting…';
    case 'frozen': return s.freezing ? 'Freezing…' : L.label(s);
    case 'paused': case 'pausing': return L.label(s);
    case 'done': case 'idle':
      if (s.subs) return waitsOn(s.subs);
      return s.detail || (s.loose ? 'Not through: ' + looseWords(s.loose) : '') || L.label(s);
    default: return s.detail || L.label(s);
  }
}

/** What a finished jot left that has not landed (DESIGN §4.8), as the phone app says it. */
function looseWords(l) {
  const n = (k, w) => `${k} ${w}${k === 1 ? '' : 's'}`;
  const parts = [];
  if (l.dirty > 0) parts.push(n(l.dirty, 'file') + ' not committed');
  if (l.ahead > 0) parts.push(n(l.ahead, 'commit') + ' not in ' + (l.land || 'its branch'));
  if (l.unproven === 'tests') parts.push('says the tests pass, but the last run failed');
  return parts.join(' · ');
}

function waitsOn(subs) {
  const parts = [];
  if (subs.helpers) parts.push(`${subs.helpers} helper${subs.helpers === 1 ? '' : 's'}`);
  if (subs.teammates) parts.push(`${subs.teammates} teammate${subs.teammates === 1 ? '' : 's'}`);
  return parts.length ? 'Waits on ' + parts.join(' and ') : 'Waits on its helpers';
}

function tokens(s) {
  const n = L.tokenTotal(s.tokens);
  return n ? L.count(n) : '';
}

function card(s, now, depth) {
  let el = cards.get(s.id);
  if (!el) {
    el = h('div', { class: 'card', role: 'option', tabindex: '-1', 'data-id': s.id, draggable: 'true' });
    el.addEventListener('click', (e) => {
      if (e.target.closest('.card-acts')) return;
      select(s.id, false);
    });
    el.addEventListener('dblclick', (e) => {
      if (e.target.closest('.card-acts')) return;
      select(s.id, true);
    });
    el.addEventListener('contextmenu', (e) => {
      e.preventDefault();
      const row = S.rows.get(el.dataset.id);
      if (row) sessionMenu(row, { x: e.clientX, y: e.clientY });
    });
    dragSource(el);
    dropTarget(el, () => ({ kind: 'card', id: el.dataset.id }));
    cards.set(s.id, el);
  }
  const lk = L.look(s);
  const loud = L.loud(lk);
  const selected = U.selected === s.id;
  const line = statusLine(s, lk);
  const helpers = (s.subagents || []).slice(0, 3);
  const more = (s.subagents || []).length - helpers.length;
  const age = L.age(s.created, now);
  const tok = tokens(s);
  const rebuilt = s.rebuilds && s.rebuilds.words ? s.rebuilds.words : '';
  const sig = JSON.stringify([s.name, lk, line, age, tok, s.parked, s.pinned, s.priority, !!s.loose, helpers, more, S.header.red_on_done, selected, s.hue, rebuilt, depth, U.renaming === s.id, s.host_down]);
  if (el._sig === sig) return el;
  el._sig = sig;
  const working = lk === 'working';
  el.className = 'card' + (loud ? ' loud' : '') + (L.quiet(lk) ? ' quiet' : '') + (working ? ' working' : '') + (selected ? ' selected' : '') +
    (s.subs ? ' waiting' : '') + (s.loose && !loud ? ' loose' : '') + (s.host_down ? ' down' : '') + ' slot' + L.slotOf(s);
  el.style.setProperty('--depth', depth);
  el.setAttribute('aria-selected', String(selected));
  el.setAttribute('aria-label', [s.name, L.label(s), line !== L.label(s) ? line : '', s.priority ? 'boosted' : '', 'started ' + L.ageSpoken(s.created, now), tok ? tok + ' tokens' : ''].filter(Boolean).join(', '));
  const name = U.renaming === s.id ? '' : `<span class="c-name">${esc(s.name)}</span>`;
  el.innerHTML =
    `<span class="${L.markClass(s, S.header.red_on_done)}">${L.glyph(s.subs ? 'working' : lk)}</span>` +
    '<div class="c-main">' +
    `<div class="c-top">${name}${s.pinned ? '<span class="c-badge" title="Pinned">📌</span>' : ''}</div>` +
    `<div class="c-line">${esc(line)}</div>` +
    (rebuilt ? `<div class="c-line c-cost">${esc(rebuilt)}</div>` : '') +
    (helpers.length ? '<div class="c-helpers">' + helpers.map((a) =>
      `<div class="c-helper${a.needs_you ? ' loud' : ''}"><span class="arrow">↳</span><b>${esc(a.type || 'Helper')}</b> ${esc(a.detail || '')}</div>`).join('') +
      (more > 0 ? `<div class="c-helper">and ${more} more helper${more === 1 ? '' : 's'}</div>` : '') + '</div>' : '') +
    '</div>' +
    `<div class="c-slot"><span class="c-age">${esc(age)}</span>${tok ? `<span class="c-tok">${esc(tok)}</span>` : ''}` +
    (s.priority ? '<span class="c-boost" title="Boosted">▲</span>' : '') + '</div>' +
    '<div class="card-acts"></div>';
  if (U.renaming === s.id) el.querySelector('.c-top').prepend(renameField(s));
  const acts = el.querySelector('.card-acts');
  const act = (label, glyph, run) => h('button', { class: 'act', title: label, 'aria-label': label, html: glyph, onclick: (e) => { e.stopPropagation(); run(); } });
  acts.append(
    act(s.frozen ? 'Resume' : 'Pause', s.frozen ? '▶' : '‖', () => op(s.frozen ? 'thaw' : 'freeze', { session: s.id })),
    act(s.parked ? 'Unpark' : 'Park', 'Ⓟ', () => op(s.parked ? 'unpark' : 'park', { session: s.id })),
    act('More', ICON.more, () => {
      const r = el.getBoundingClientRect();
      sessionMenu(s, { x: r.right - 8, y: r.bottom - 6 });
    }));
  return el;
}

function outsideRow(o) {
  const key = 'out:' + (o.vendor_id || o.id);
  let el = cards.get(key);
  if (!el) {
    el = h('div', { class: 'card outside', role: 'option', tabindex: '-1' });
    el.addEventListener('click', () => {
      U.outsideSelected = key;
      U.selected = '';
      schedule();
    });
    el.addEventListener('contextmenu', (e) => {
      e.preventDefault();
      menu([{ label: 'Move into Irie', disabled: !o.movable, run: () => moveIn(o) }, o.why ? { label: o.why, disabled: true } : null], { x: e.clientX, y: e.clientY });
    });
    cards.set(key, el);
  }
  const sig = JSON.stringify([o.name, o.status, o.waiting_for, o.movable, U.outsideSelected === key]);
  if (el._sig !== sig) {
    el._sig = sig;
    el.classList.toggle('selected', U.outsideSelected === key);
    const what = o.waiting_for ? 'Waits for ' + o.waiting_for : o.status || o.state || o.kind;
    el.innerHTML = `<span class="mark r-muted">${L.glyph(o.status === 'waiting' ? 'needs' : 'idle')}</span>` +
      `<div class="c-main"><div class="c-top"><span class="c-name">${esc(o.name || L.base(o.cwd) || 'Claude Code')}</span></div>` +
      `<div class="c-line">${esc(what || '')} · ${esc(L.base(o.cwd))}</div></div>` +
      (o.movable ? '<div class="c-slot"><button class="btn small">Move in</button></div>' : '');
    const b = el.querySelector('.btn');
    if (b) b.addEventListener('click', (e) => {
      e.stopPropagation();
      moveIn(o);
    });
  }
  return el;
}

async function moveIn(o) {
  try {
    const r = await call('move', { vendor_id: o.vendor_id }, { timeout: 60000 });
    if (r.session) select(r.session.id, true);
  } catch (e) {
    notice(e.message, { error: true });
  }
}

function closedRow(c, now) {
  const key = 'closed:' + c.id;
  let el = cards.get(key);
  if (!el) {
    el = h('div', { class: 'card closed', role: 'option', tabindex: '-1' });
    cards.set(key, el);
  }
  const sig = JSON.stringify([c.name, c.closed, Math.floor(now / 60000), c.cold && c.cold.words]);
  if (el._sig !== sig) {
    el._sig = sig;
    el.innerHTML = `<span class="mark r-muted">${L.glyph('exited')}</span>` +
      `<div class="c-main"><div class="c-top"><span class="c-name">${esc(c.name)}</span></div>` +
      `<div class="c-line">${esc((c.auto ? 'Closed itself ' : 'Closed ') + L.ageSpoken(c.closed, now) + (c.cold && c.cold.words ? ' · ' + c.cold.words : ''))}</div></div>` +
      '<div class="c-slot"><button class="btn small">Reopen</button></div>';
    el.querySelector('.btn').addEventListener('click', () => reopen(c.id));
  }
  return el;
}

function emptyNote() {
  let el = cards.get('empty');
  if (!el) {
    el = h('div', { class: 'empty' });
    cards.set('empty', el);
  }
  const text = U.find ? 'No session matches.' : S.loaded ? 'No sessions yet. Type a task below to start one.' : 'Loading the sessions…';
  if (el.textContent !== text) el.textContent = text;
  return el;
}

function renameField(s) {
  const input = h('input', { class: 'rename', value: s.name, 'aria-label': 'New name', spellcheck: 'false' });
  const end = async (commit) => {
    if (U.renaming !== s.id) return;
    U.renaming = '';
    const name = input.value.trim();
    if (commit && name && name !== s.name) await op('rename', { session: s.id, name });
    schedule();
  };
  input.addEventListener('keydown', (e) => {
    e.stopPropagation();
    if (e.key === 'Enter') end(true);
    if (e.key === 'Escape') end(false);
  });
  input.addEventListener('blur', () => end(true));
  requestAnimationFrame(() => {
    input.focus();
    input.select();
  });
  return input;
}

function startRename(id) {
  U.renaming = id;
  schedule();
}

// ---- Dragging cards ---------------------------------------------------------------

let dragging = '';

function dragSource(el) {
  el.addEventListener('dragstart', (e) => {
    dragging = el.dataset.id;
    e.dataTransfer.effectAllowed = 'move';
    e.dataTransfer.setData('text/plain', dragging);
    el.classList.add('dragging');
  });
  el.addEventListener('dragend', () => {
    el.classList.remove('dragging');
    dragging = '';
    for (const x of refs.list.querySelectorAll('.drop-before, .drop-into')) x.classList.remove('drop-before', 'drop-into');
  });
}

function dropTarget(el, what) {
  el.addEventListener('dragover', (e) => {
    if (!dragging) return;
    const t = what();
    if (t.kind === 'card' && t.id === dragging) return;
    if (t.kind === '') return;
    e.preventDefault();
    e.dataTransfer.dropEffect = 'move';
    el.classList.add(t.kind === 'card' ? 'drop-before' : 'drop-into');
  });
  el.addEventListener('dragleave', () => el.classList.remove('drop-before', 'drop-into'));
  el.addEventListener('drop', (e) => {
    e.preventDefault();
    el.classList.remove('drop-before', 'drop-into');
    const id = dragging;
    if (!id) return;
    const t = what();
    dropped(id, t);
  });
}

/** A drop is reorder, or across Parked's or Pinned's edge park or pin, or into a group (CORE.md §2). */
function dropped(id, t) {
  const s = S.rows.get(id);
  if (!s) return;
  if (t.kind === 'park') return op(s.parked ? 'reorder' : 'park', { session: id });
  if (t.kind === 'pin') return op(s.pinned ? 'reorder' : 'pin', { session: id });
  if (t.kind === 'group' && t.group) return op('group', { do: 'add', group: t.group.id, sessions: [id] });
  if (t.kind !== 'card') return;
  const target = S.rows.get(t.id);
  if (!target) return;
  if (!!target.parked !== !!s.parked) return op(target.parked ? 'park' : 'unpark', { session: id, before: t.id });
  if (!!target.pinned !== !!s.pinned) return op(target.pinned ? 'pin' : 'unpin', { session: id, before: t.id });
  if ((target.group || '') !== (s.group || '') && U.view === 'list') {
    if (target.group) return op('group', { do: 'add', group: target.group, sessions: [id], before: t.id });
    return op('group', { do: 'remove', sessions: [id] }).then(() => op('reorder', { session: id, before: t.id }));
  }
  return op('reorder', { session: id, before: t.id });
}

// ---- Selecting and showing -------------------------------------------------------------

let showing = false, wanted = null;

/** Shows a session in the terminal; presses faster than the core answers collapse into the last. */
function select(id, focus) {
  if (!id) return;
  U.outsideSelected = '';
  U.selected = id;
  schedule();
  wanted = { id, focus };
  if (showing) return;
  showing = true;
  (async () => {
    while (wanted) {
      const next = wanted;
      wanted = null;
      try {
        await call('show', { session: next.id, focus: false }, { timeout: 10000 });
        if (next.focus && !wanted) term.focus();
      } catch (e) {
        if (!wanted) notice(e.message, { error: true });
      }
    }
    showing = false;
  })();
  const el = cards.get(id);
  if (el) requestAnimationFrame(() => el.scrollIntoView({ block: 'nearest' }));
}

function step(d) {
  const ids = walk(sections, folded);
  if (!ids.length) return;
  const i = ids.indexOf(U.selected);
  const next = i < 0 ? (d > 0 ? ids[0] : ids[ids.length - 1]) : ids[(i + d + ids.length) % ids.length];
  select(next, false);
}

function nth(n) {
  const ids = walk(sections, folded);
  if (ids[n]) select(ids[n], false);
}

// ---- The ops ------------------------------------------------------------------------

const UNDO_VERB = { park: 'Parked', unpark: 'Unparked', pin: 'Pinned', unpin: 'Unpinned', freeze: 'Paused', thaw: 'Resumed', close: 'Closed', priority: 'Boosted', rename: 'Renamed', reorder: 'Moved', group: 'Grouped' };

/** One op; its error in the core's words, with an Undo where the history took a step. */
async function op(name, args, { quiet = false } = {}) {
  try {
    const r = await call(name, args, { timeout: 20000 });
    const s = args.session && S.rows.get(args.session);
    if (!quiet && UNDO_VERB[name] && s && name !== 'reorder' && name !== 'rename') {
      notice(`${UNDO_VERB[name]} “${s.name}”`, { action: { label: 'Undo', run: () => history('undo') } });
    }
    return r;
  } catch (e) {
    notice(e.message, { error: true });
    return null;
  }
}

async function closeSessions(ids) {
  const list = ids.map((id) => S.rows.get(id)).filter(Boolean);
  if (!list.length) return;
  try {
    await call('close', list.length === 1 ? { session: list[0].id } : { sessions: list.map((s) => s.id) }, { timeout: 30000 });
    notice(list.length === 1 ? `Closed “${list[0].name}”` : `Closed ${list.length} sessions`, { action: { label: 'Undo', run: () => history('undo') } });
    if (list.some((s) => s.id === U.selected)) U.selected = '';
  } catch (e) {
    notice(e.message, { error: true });
  }
}

async function reopen(id) {
  try {
    const r = await call('reopen', id ? { session: id } : {}, { timeout: 60000 });
    if (r.session) select(r.session.id, false);
  } catch (e) {
    notice(e.message, { error: true });
  }
}

/** Undo or redo; an uncertain one asks the core's question first (DESIGN §4.5). */
async function history(which, choice) {
  try {
    const r = await call(which, choice || {}, { timeout: 30000 });
    if (r.did === 'asked' && r.choice) return askChoice(which, r.choice);
    const words = { undone: 'Undid', redone: 'Redid', settled: 'Settled', skipped: 'Skipped', cancelled: 'Left as it was' }[r.did] || '';
    if (words) notice(`${words} ${r.label || ''}`.trim());
  } catch (e) {
    notice(e.message, { error: true });
  }
}

function askChoice(which, c) {
  const opts = h('div', { class: 'choices' });
  const d = dialog({ title: c.question, content: opts, buttons: [{ label: 'Cancel' }] });
  for (const o of c.options || []) {
    opts.append(h('button', {
      class: 'choice' + (o.recommended ? ' recommended' : ''),
      onclick: () => {
        d.close();
        history(which, { choice_id: c.id, choice: o.key });
      },
    }, h('b', { text: o.label }), h('span', { text: o.detail || '' })));
  }
}

function sessionMenu(s, at) {
  const lk = L.look(s);
  const finished = lk === 'done' || lk === 'idle' || lk === 'exited' || lk === 'exitedError';
  menu([
    { label: 'Show', key: 'Enter', run: () => select(s.id, true) },
    { label: 'Rename…', key: 'F2', run: () => startRename(s.id) },
    '-',
    { label: s.pinned ? 'Unpin' : 'Pin to the top', run: () => op(s.pinned ? 'unpin' : 'pin', { session: s.id }) },
    { label: s.parked ? 'Unpark' : 'Park', run: () => op(s.parked ? 'unpark' : 'park', { session: s.id }) },
    { label: s.frozen ? 'Resume' : 'Pause', run: () => op(s.frozen ? 'thaw' : 'freeze', { session: s.id }) },
    { label: s.priority ? 'Stop boosting' : 'Boost', run: () => op('priority', { session: s.id, level: s.priority ? 0 : 1 }) },
    '-',
    { label: 'Put in a new group', run: () => op('group', { do: 'new', sessions: [s.id] }) },
    s.group ? { label: 'Take out of its group', run: () => op('group', { do: 'remove', sessions: [s.id] }) } : null,
    '-',
    finished ? { label: 'Rate: it went well', run: () => op('rate', { session: s.id, verdict: 'good' }, { quiet: true }).then((r) => r && notice('Thanks: noted.')) } : null,
    finished ? { label: 'Rate: it went badly', run: () => op('rate', { session: s.id, verdict: 'bad' }, { quiet: true }).then((r) => r && notice('Thanks: noted.')) } : null,
    finished ? '-' : null,
    { label: 'Close', key: 'Ctrl+Shift+W', danger: true, run: () => closeSessions([s.id]) },
    { label: 'Close the others', run: () => typed(':close others', s.id) },
    { label: 'Close all finished', run: () => typed(':close finished', s.id) },
  ].filter((x, i, a) => !(x === '-' && (a[i - 1] === '-' || i === 0))), { ...at, label: s.name });
}

function groupMenu(e, g) {
  menu([
    { label: 'Rename group…', run: () => ask({ title: 'Rename the group', value: g.name, run: (name) => call('group', { do: 'rename', group: g.id, name }) }) },
    { label: 'Ungroup', run: () => op('group', { do: 'ungroup', group: g.id }) },
  ], { x: e.clientX, y: e.clientY, label: g.name });
}

function appMenu(anchor) {
  menu([
    { label: 'New task', key: 'Ctrl+Shift+N', run: focusTask },
    { label: 'Reopen the last closed', key: 'Ctrl+Shift+T', run: () => reopen('') },
    '-',
    { label: S.undo ? `Undo ${S.undo}` : 'Undo', key: 'Ctrl+Z', disabled: !S.undo, run: () => history('undo') },
    { label: S.redo ? `Redo ${S.redo}` : 'Redo', key: 'Ctrl+Y', disabled: !S.redo, run: () => history('redo') },
    '-',
    { label: 'List', checked: U.view === 'list', run: () => setView('list') },
    { label: 'Repo', checked: U.view === 'repo', run: () => setView('repo') },
    '-',
    { label: 'Pause all', run: () => typed(':freeze all') },
    { label: 'Resume all', run: () => typed(':thaw all') },
    '-',
    { label: 'Settings', key: 'Ctrl+,', run: settings },
    { label: 'Keyboard shortcuts', key: 'F1', run: keysHelp },
    { label: 'Irie’s guide', run: () => host.open('https://irie-people.github.io/irie/') },
  ], { anchor, label: 'Menu' });
}

function settings() {
  openSettings({ call, notice, link: U.link, version: U.version, features: U.features, input: typed, header: () => S.header, open: host.open });
}

function setView(v) {
  U.view = v;
  store.set('view', v);
  schedule();
}

// ---- The details and the head ---------------------------------------------------------

function drawDetails() {
  const s = S.rows.get(U.selected);
  refs.details.hidden = !s;
  if (!s) return;
  const lk = L.look(s);
  const where = [s.host ? 'on ' + s.host : '', s.repo ? L.base(s.repo) : s.cwd ? L.base(s.cwd) : '', s.branch].filter(Boolean).join(' · ');
  const agent = [s.profile ? '+' + s.profile : '', s.model, s.local ? 'local' : ''].filter(Boolean).join(' · ');
  const t = s.tokens;
  const spend = [t ? `${L.count(L.tokenTotal(t))} tokens` : '', s.cost_usd != null ? '$' + s.cost_usd.toFixed(2) : '', s.context != null ? Math.round(s.context) + '% context' : ''].filter(Boolean).join(' · ');
  const sig = JSON.stringify([s.id, lk, s.detail, s.command, where, agent, spend, s.warnings, s.reason, s.tail, s.loose]);
  if (refs.details._sig === sig) return;
  refs.details._sig = sig;
  const line = (label, value, cls = '') => value ? h('div', { class: 'd-line ' + cls }, h('span', { class: 'd-label', text: label }), h('span', { class: 'd-value', text: value, title: value })) : null;
  fill(refs.details, 
    h('div', { class: 'd-state' }, h('span', { class: L.markClass(s, S.header.red_on_done), html: L.glyph(lk) }), h('b', { text: L.label(s) }),
      s.reason ? h('span', { class: 'd-reason', text: ' · ' + s.reason }) : null),
    line('Step', s.command || s.detail || ''),
    line('Where', where),
    line('Agent', agent),
    line('Spent', spend),
    s.loose ? line('Not through', looseWords(s.loose), 'accent') : null,
    ...(s.warnings || []).map((w) => line('Note', w, 'warn')),
    s.tail && s.tail.length ? h('pre', { class: 'd-tail', text: s.tail.join('\n') }) : null);
}

function drawHead() {
  const s = S.rows.get(U.selected);
  refs.frame.className = 'frame' + (s ? ' slot' + L.slotOf(s) + ' r-' + L.role(L.look(s), S.header.red_on_done) : '');
  const sig = JSON.stringify(s ? [s.id, s.name, L.look(s), s.detail, s.frozen, s.parked, s.priority, s.branch, s.repo, s.hue] : null);
  if (refs.head._sig === sig) return;
  refs.head._sig = sig;
  if (!s) {
    refs.headMark.className = 'mark';
    refs.headMark.innerHTML = '';
    refs.headName.textContent = S.rows.size ? 'Pick a session' : 'Irie';
    refs.headLine.textContent = S.rows.size ? 'Its terminal shows here.' : 'Type a task in the task bar to start an agent.';
    fill(refs.headActs, );
    return;
  }
  const lk = L.look(s);
  refs.headMark.className = L.markClass(s, S.header.red_on_done);
  refs.headMark.innerHTML = L.glyph(lk);
  refs.headName.textContent = s.name;
  refs.headLine.textContent = [L.label(s), s.detail && s.detail !== L.label(s) ? s.detail : '', s.branch].filter(Boolean).join(' · ');
  const b = (label, glyph, run, on = false) => h('button', { class: 'head-btn' + (on ? ' on' : ''), title: label, 'aria-label': label, 'aria-pressed': on ? 'true' : null, html: glyph, onclick: run });
  fill(refs.headActs, 
    b(s.frozen ? 'Resume' : 'Pause', s.frozen ? '▶' : '‖', () => op(s.frozen ? 'thaw' : 'freeze', { session: s.id }), !!s.frozen),
    b(s.priority ? 'Stop boosting' : 'Boost', '▲', () => op('priority', { session: s.id, level: s.priority ? 0 : 1 }), !!s.priority),
    b(s.parked ? 'Unpark' : 'Park', 'Ⓟ', () => op(s.parked ? 'unpark' : 'park', { session: s.id }), !!s.parked),
    b('More', ICON.more, (e) => sessionMenu(s, { x: e.currentTarget.getBoundingClientRect().right - 4, y: e.currentTarget.getBoundingClientRect().bottom + 4 })));
}

function drawCounts() {
  const c = counts([...S.rows.values()]);
  const sig = JSON.stringify(c);
  if (refs.counts._sig === sig) return;
  refs.counts._sig = sig;
  const chip = (key, n, lk, words) => n ? h('button', {
    class: 'count ' + key, title: `${n} ${words}`, 'aria-label': `${n} ${words}`,
    onclick: () => c.first[key] && select(c.first[key], false),
  }, h('span', { class: 'mark ' + markOf(lk), html: L.glyph(lk) }), String(n)) : null;
  fill(refs.counts, ...[
    chip('needs', c.needs, 'needs', c.needs === 1 ? 'needs you' : 'need you'),
    chip('working', c.working, 'working', 'working'),
    chip('verify', c.verify, 'done', 'to verify'),
    chip('paused', c.paused, 'paused', 'paused'),
    chip('frozen', c.frozen, 'frozen', 'frozen'),
  ].filter(Boolean));
}

function markOf(lk) {
  return 'r-' + L.role(lk, S.header.red_on_done);
}

function title() {
  const c = counts([...S.rows.values()]);
  const t = U.link.state !== 'ready' ? 'Irie' : c.needs ? `Irie — ${c.needs} need${c.needs === 1 ? 's' : ''} you` : 'Irie';
  if (document.title !== t) {
    document.title = t;
    host.title(t);
  }
}

// ---- Notifications --------------------------------------------------------------------

/** A session that comes to need you while the window is not in front says so. */
function notifyChanges() {
  for (const s of S.rows.values()) {
    const loud = needsYou(s) && !s.parked;
    const was = U.lastLoud.get(s.id);
    U.lastLoud.set(s.id, loud);
    if (loud && was === false && !document.hasFocus()) host.notify(`${s.name} needs you`, s.detail || L.label(s));
  }
  for (const id of U.lastLoud.keys()) if (!S.rows.has(id)) U.lastLoud.delete(id);
}

// ---- The task bar ----------------------------------------------------------------------

function focusTask() {
  refs.taskInput.focus();
}

function typedWords(text) {
  const first = String(text || '').split('\n')[0];
  const repo = first.match(/(?:^|\s)@(\S+)/);
  const prof = first.match(/(?:^|\s)\+(\S+)/);
  return { repo: repo && repo[1], profile: prof && prof[1] };
}

function drawChips() {
  const typed = typedWords(refs.taskInput.value);
  const key = JSON.stringify([typed, U.task]);
  if (refs.taskChips._key === key) return;
  refs.taskChips._key = key;
  const chip = (sym, text, set, label, run) => h('button', { class: 'tchip' + (set ? ' set' : ''), 'aria-label': label, title: label, onclick: run },
    h('span', { class: 'sym', text: sym }), h('span', { text }), h('span', { class: 'caret', html: ICON.caret }));
  const repo = typed.repo || U.task.repo || '';
  const prof = typed.profile || U.task.profile || '';
  fill(refs.taskChips, 
    chip('@', typed.repo ? typed.repo : repo ? L.base(repo) : 'Not in a repo', !!repo, `Where it runs: ${repo || 'a folder of its own'}`, pickRepo),
    chip('+', prof || 'Default agent', !!prof, `Agent: ${prof || 'the default'}`, pickProfile),
    h('span', { class: 'task-hint', text: 'Enter starts · Shift+Enter, a new line' }));
}

function saveTask() {
  store.set('task', U.task);
  drawChips();
}

function pickRepo() {
  picker({
    title: 'Where it runs',
    current: U.task.repo || '',
    load: async () => {
      const r = await call('complete', { text: '@' }, { timeout: 8000 });
      const items = (r.items || []).map((i) => i.replace(/^@/, ''));
      return [{ value: '', label: 'Not in a repo', note: 'A folder of its own' }, ...items.map((name) => ({ value: name, label: '@' + name, note: 'Its own worktree' }))];
    },
    choose: (v) => {
      U.task.repo = v;
      saveTask();
      focusTask();
    },
  });
}

function pickProfile() {
  picker({
    title: 'Agent',
    current: U.task.profile || '',
    load: async () => {
      const r = await call('complete', { text: '+' }, { timeout: 8000 });
      const notes = r.notes || [];
      return [{ value: '', label: 'The default', note: 'As the config says' }, ...(r.items || []).map((i, k) => ({ value: i.replace(/^\+/, ''), label: i, note: notes[k] || '' }))];
    },
    choose: (v) => {
      U.task.profile = v;
      saveTask();
      focusTask();
    },
  });
}

let historyAt = -1;

async function submitTask() {
  const text = refs.taskInput.value.trim();
  if (!text || refs.taskSend.classList.contains('busy')) return;
  refs.taskInput.value = '';
  grow();
  drawChips();
  U.history = [text, ...U.history.filter((x) => x !== text)].slice(0, 50);
  store.set('taskHistory', U.history);
  historyAt = -1;
  refs.taskSend.classList.add('busy');
  try {
    await typed(text, U.selected, true);
  } finally {
    refs.taskSend.classList.remove('busy');
    refs.taskSend.disabled = !refs.taskInput.value.trim();
  }
}

/** What is typed, to the core's `input` (CORE.md §3), as typed, with the selection and the menus. */
async function typed(text, session = U.selected, fromBar = false) {
  const args = { text };
  if (session) args.session = session;
  if (U.task.repo) args.repo = U.task.repo;
  if (U.task.profile) args.profile = U.task.profile;
  if (U.outsideSelected) args.outside = U.outsideSelected.replace(/^out:/, '');
  try {
    const r = await call('input', args, { timeout: 120000 });
    const sid = idOf(r.session);
    if (r.did === 'asked' && r.choice && r.op) return askChoice(r.op, r.choice);
    if (r.did === 'spawned' && sid) {
      // A new agent starts behind the session on show; only when nothing is
      // on show does it take the terminal (docs/MAC.md §1).
      if (r.show || !U.selected) select(sid, !!r.show);
      notice(r.text || 'Started.', { action: U.selected !== sid ? { label: 'Show', run: () => select(sid, true) } : null });
    } else if (r.did === 'added' && sid) {
      notice(r.text || 'Added to its last task.', { action: { label: 'Start it apart', run: () => call('input', { ...args, apart: sid }).catch((e) => notice(e.message, { error: true })) } });
    } else if (sid && r.show) {
      select(sid, true);
      if (r.text) notice(r.text);
    } else if (r.text) {
      notice(r.text, { ms: r.did === 'answered' ? 9000 : 4500 });
    }
    return r;
  } catch (e) {
    // One the core refuses comes back into an empty bar.
    if (fromBar && !refs.taskInput.value.trim()) {
      refs.taskInput.value = text;
      grow();
      drawChips();
    }
    notice(e.message, { error: true });
    return null;
  }
}

function grow() {
  const t = refs.taskInput;
  t.style.height = 'auto';
  t.style.height = Math.min(t.scrollHeight, 160) + 'px';
  refs.taskSend.disabled = !t.value.trim();
}

// ---- The terminal -----------------------------------------------------------------------

let term;

function makeTerminal() {
  term = new SlotTerminal(refs.termEl, {
    keys: (e) => appKey(e, true),
    onNote: (text) => termNote(text),
    onState: (state, text) => {
      refs.termCover.hidden = state === 'attached';
      if (state !== 'attached') fill(refs.termCover, h('div', { class: 'term-cover-text' }, h('span', { class: 'spin-ring', html: ICON.ring }), text || 'Attaching…'));
    },
  });
  term.menu = (e, t) => menu([
    { label: 'Copy', key: 'Ctrl+C', disabled: !t.term.hasSelection(), run: () => t.copySelection(false) },
    { label: 'Paste', key: 'Ctrl+V', run: () => t.paste() },
    { label: 'Select all', run: () => t.selectAll() },
    '-',
    { label: 'Bigger text', key: 'Ctrl+=', run: () => t.zoom(1) },
    { label: 'Smaller text', key: 'Ctrl+-', run: () => t.zoom(-1) },
    { label: 'Default size', key: 'Ctrl+0', run: () => t.zoom(0) },
  ], { x: e.clientX, y: e.clientY, label: 'Terminal' });
  refs.termCover.hidden = false;
  fill(refs.termCover, h('div', { class: 'term-cover-text' }, h('span', { class: 'spin-ring', html: ICON.ring }), 'Attaching…'));
}

let noteTimer;

function termNote(text) {
  refs.termNote.textContent = text;
  refs.termNote.hidden = false;
  clearTimeout(noteTimer);
  noteTimer = setTimeout(() => (refs.termNote.hidden = true), 1600);
}

// ---- Keys --------------------------------------------------------------------------------

/**
 * The app's keys, from anywhere. In the terminal, a plain Ctrl key is the
 * agent's; the app's own there have Shift too, as Windows Terminal's do.
 * Gives true when it took the key.
 */
function appKey(e, inTerm) {
  const k = e.key;
  const lower = k.length === 1 ? k.toLowerCase() : k;
  const ctrl = e.ctrlKey && !e.metaKey, shift = e.shiftKey, alt = e.altKey;
  const take = (fn) => {
    e.preventDefault();
    fn();
    return true;
  };
  if (isOpen()) return false;
  if (ctrl && k === 'Tab') return take(() => step(shift ? -1 : 1));
  if (ctrl && !alt && (k === ']' || e.code === 'BracketRight')) return take(() => toggleFocus());
  if (ctrl && alt && /^[1-9]$/.test(k)) {
    const sp = (S.header.spaces || [])[Number(k) - 1];
    if (sp) return take(() => setSpace(Number(k) === 1 ? '' : sp.id || sp.name));
  }
  if (ctrl && !alt && !shift && /^[1-9]$/.test(k)) return take(() => nth(Number(k) - 1));
  if (ctrl && !alt && (k === '=' || k === '+')) return take(() => term.zoom(1));
  if (ctrl && !alt && k === '-') return take(() => term.zoom(-1));
  if (ctrl && !alt && k === '0') return take(() => term.zoom(0));
  if (ctrl && !alt && k === ',') return take(settings);
  if (k === 'F1') return take(keysHelp);
  if (ctrl && shift && !alt) {
    switch (lower) {
      case 'n': return take(focusTask);
      case 'f': return take(() => refs.find.focus());
      case 't': return take(() => reopen(''));
      case 'w': return take(() => U.selected && closeSessions([U.selected]));
      case 'z': if (!inTerm) return take(() => history('redo')); break;
      case 'e': return take(() => U.selected && startRename(U.selected));
    }
  }
  if (inTerm) return false;
  // Outside the terminal, the plain keys too.
  if (ctrl && !shift && !alt) {
    switch (lower) {
      case 'n': return take(focusTask);
      case 'f': return take(() => refs.find.focus());
      case 'z': return take(() => history('undo'));
      case 'y': return take(() => history('redo'));
    }
  }
  return false;
}

function toggleFocus() {
  if (term.hasFocus()) refs.list.focus();
  else term.focus();
}

function keysHelp() {
  const rows = [
    ['Ctrl+Tab, Ctrl+Shift+Tab', 'Show the next or the previous session'],
    ['Ctrl+]', 'Move the keyboard between the list and the terminal'],
    ['Ctrl+1 to Ctrl+9', 'Show the first to ninth session'],
    ['↑ ↓, then Enter', 'Pick a session in the list, then type to it'],
    ['Ctrl+Shift+N', 'Type a task in the task bar'],
    ['Ctrl+Shift+F', 'Find a session; Esc clears it'],
    ['F2, Ctrl+Shift+E', 'Rename the selected session'],
    ['Ctrl+Shift+W, Delete', 'Close the selected session (Delete in the list)'],
    ['Ctrl+Shift+T', 'Reopen the session closed last'],
    ['Ctrl+Z, Ctrl+Y', 'Undo or redo, outside the terminal'],
    ['Alt+Shift+↑ ↓', 'Move the selected card up or down'],
    ['Ctrl+Alt+1 to 9', 'Show that space'],
    ['Ctrl+C, Ctrl+V', 'Copy the selection, or paste, in the terminal'],
    ['Drag, Shift+drag', 'Select in tmux (copied as you let go), or in the terminal itself'],
    ['Ctrl+=, Ctrl+-, Ctrl+0', 'Bigger, smaller or the default terminal text'],
    ['Ctrl+,', 'Settings'],
  ];
  dialog({
    title: 'Keyboard shortcuts', wide: true,
    content: h('table', { class: 'keys' }, h('tbody', {}, ...rows.map(([k, v]) => h('tr', {}, h('td', { class: 'k', text: k }), h('td', { text: v }))))),
    buttons: [{ label: 'Done', primary: true }],
  });
}

// ---- Wiring --------------------------------------------------------------------------------

function wire() {
  document.addEventListener('keydown', (e) => {
    if (term && term.hasFocus()) return; // the terminal asked appKey itself
    if (appKey(e, false)) return;
    const inField = e.target.closest && e.target.closest('input, textarea, [contenteditable]');
    if (inField) return;
    if (e.key === 'F2' && U.selected) {
      e.preventDefault();
      startRename(U.selected);
    }
  }, true);
  // The list: arrows pick, Enter types to it, Delete closes, Alt+Shift+arrows move.
  refs.list.addEventListener('keydown', (e) => {
    if (e.target !== refs.list && !e.target.classList.contains('card')) return;
    if (e.altKey && e.shiftKey && (e.key === 'ArrowUp' || e.key === 'ArrowDown')) {
      e.preventDefault();
      moveCard(e.key === 'ArrowUp' ? -1 : 1);
      return;
    }
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault();
      step(e.key === 'ArrowDown' ? 1 : -1);
    } else if (e.key === 'Enter' && U.selected) {
      e.preventDefault();
      select(U.selected, true);
    } else if (e.key === 'Delete' && U.selected) {
      e.preventDefault();
      closeSessions([U.selected]);
    } else if (e.key === 'F2' && U.selected) {
      e.preventDefault();
      startRename(U.selected);
    }
  });
  refs.find.addEventListener('input', () => {
    U.find = refs.find.value.trim();
    schedule();
  });
  refs.find.addEventListener('keydown', (e) => {
    if (e.key === 'Escape') {
      refs.find.value = '';
      U.find = '';
      schedule();
      refs.list.focus();
    } else if (e.key === 'Enter' || e.key === 'ArrowDown') {
      e.preventDefault();
      const ids = walk(sections, folded);
      if (ids[0]) select(ids[0], e.key === 'Enter');
      refs.list.focus();
    }
  });
  refs.taskInput.addEventListener('input', () => {
    grow();
    drawChips();
  });
  refs.taskInput.addEventListener('keydown', (e) => {
    if (e.key === 'Enter' && !e.shiftKey && !e.altKey) {
      e.preventDefault();
      submitTask();
    } else if (e.key === 'ArrowUp' && !refs.taskInput.value.trim() && U.history.length) {
      e.preventDefault();
      historyAt = Math.min(historyAt + 1, U.history.length - 1);
      refs.taskInput.value = U.history[historyAt];
      grow();
      drawChips();
    } else if (e.key === 'Escape') {
      refs.taskInput.blur();
      if (term) term.focus();
    }
  });
  refs.taskSend.addEventListener('click', submitTask);
  // The sidebar's edge: the terminal keeps its size while it is dragged, and takes the new one once (docs/MAC.md §2).
  refs.divider.addEventListener('pointerdown', (e) => {
    e.preventDefault();
    refs.divider.setPointerCapture(e.pointerId);
    term && term.setHold(true);
    const x0 = e.clientX, w0 = refs.side.offsetWidth;
    const move = (ev) => {
      U.side = Math.max(240, Math.min(560, w0 + ev.clientX - x0));
      refs.side.style.width = U.side + 'px';
    };
    const up = () => {
      refs.divider.removeEventListener('pointermove', move);
      refs.divider.removeEventListener('pointerup', up);
      store.set('side', U.side);
      term && term.setHold(false);
    };
    refs.divider.addEventListener('pointermove', move);
    refs.divider.addEventListener('pointerup', up);
  });
  refs.divider.addEventListener('dblclick', () => {
    U.side = 300;
    refs.side.style.width = '300px';
    store.set('side', 300);
    term && term.layout();
  });
  // Ages move on by themselves.
  setInterval(schedule, 30000);
  window.addEventListener('focus', () => schedule());
  makeTerminal();
  grow();
  drawChips();
}

function moveCard(d) {
  const ids = walk(sections, folded);
  const i = ids.indexOf(U.selected);
  if (i < 0) return;
  if (d < 0 && i > 0) op('reorder', { session: U.selected, before: ids[i - 1] });
  if (d > 0 && i < ids.length - 1) op('reorder', { session: U.selected, before: ids[i + 2] || '' });
}

// ---- Start ---------------------------------------------------------------------------------

build();
paint();
render();
drawLink();
hello();

// A test or the preview can look at the page's state.
window.__irie = { S, U, render: () => render() };
export { ApiError };
