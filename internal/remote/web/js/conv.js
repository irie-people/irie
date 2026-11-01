// One session's screen: its conversation (DESIGN §4.7), drawn from the
// core's `conversation` frames and patched by item id, and at its foot the
// message box, or the needs card while the agent waits on you in its
// terminal, or why it can't take a message now.

import { h, ICON, actions, toast } from './ui.js';
import { md, esc } from './md.js';
import * as L from './look.js';
import { Stream, call } from './net.js';

const GAP = 30 * 60e3; // a time line goes between items this far apart

export class SessionView {
  constructor(ctx, conn, sid) {
    this.ctx = ctx;
    this.conn = conn;
    this.sid = sid;
    this.items = [];
    this.index = new Map();
    this.versions = new Map(); // id → {json, v}
    this.needs = null;
    this.pending = [];
    this.more = false;
    this.transcript = null;
    this.loaded = false;
    this.problem = '';
    this.expanded = new Set();
    this.blockEls = new Map();
    this.lastKey = '';
    this.mode = '';
    this.screen = null; // {lines, options} from the screen op
    this.screenProblem = '';
    this.showTerm = false;
    this.showKeys = false;
    this.busyKey = null;
    this.sending = false;
    this.scheduled = false;
    this.el = this.build();
    this.stream = new Stream(conn.m, { session: sid, conversation: true }, {
      frame: (f) => this.onFrame(f),
      unpaired: (msg) => {
        this.problem = msg || `${conn.m.name} no longer knows this phone.`;
        this.invalidate();
        ctx.unpaired(conn, msg);
      },
      refused: (e) => {
        this.problem = e.message;
        this.invalidate();
        return e.code === 'not_found' || e.code === 'bad_args';
      },
    });
    this.stream.start();
    this.onVisible = () => {
      if (document.visibilityState === 'visible' && this.mode === 'needs') this.poll(true);
    };
    document.addEventListener('visibilitychange', this.onVisible);
    this.invalidate();
  }

  get row() {
    return this.conn.sessions.get(this.sid) || null;
  }

  destroy() {
    this.destroyed = true;
    if (this.ro) this.ro.disconnect();
    this.stream.stop();
    clearTimeout(this.pollTimer);
    this.pollTimer = null;
    document.removeEventListener('visibilitychange', this.onVisible);
  }

  // ---- Building -------------------------------------------------------------

  build() {
    this.nameEl = h('span', { class: 'nav-name' });
    this.subEl = h('span', { class: 'nav-sub' });
    this.titleBtn = h('button', { class: 'nav-title', 'aria-expanded': 'false', onclick: () => this.toggleDetails() }, this.nameEl, this.subEl);
    const header = h('header', { class: 'bar nav on-dark' },
      h('button', { class: 'icon-btn back', 'aria-label': 'Back to the sessions', html: ICON.back, onclick: () => this.ctx.back() }),
      this.titleBtn,
      h('button', { class: 'icon-btn', 'aria-label': 'Actions for this session', html: ICON.more, onclick: () => this.openActions() }));
    this.detailsEl = h('div', { class: 'details', hidden: true });
    this.moreEl = h('div', { class: 'conv-more', hidden: true, text: 'Earlier messages are in its terminal.' });
    // Quiet until the first frame is drawn, so VoiceOver doesn't read the whole history.
    this.convEl = h('div', { class: 'conv', role: 'log', 'aria-live': 'off', 'aria-relevant': 'additions' });
    this.emptyEl = h('div', { class: 'empty', hidden: true });
    this.scrollEl = h('div', { class: 'scroll feed' }, this.moreEl, this.convEl, this.emptyEl);
    this.pill = h('button', { class: 'new-pill', hidden: true, onclick: () => this.toBottom(true) }, h('span', { html: ICON.down }), 'New');
    // Stuck to the bottom while the person is there: new items and blocks
    // that settle their height keep it there, until they scroll up.
    this.stuck = true;
    this.scrollEl.addEventListener('scroll', () => {
      const near = this.nearBottom();
      this.stuck = near;
      if (near) this.pill.hidden = true;
    }, { passive: true });
    this.scrollEl.addEventListener('touchstart', () => (this.touching = true), { passive: true });
    this.scrollEl.addEventListener('touchend', () => (this.touching = false), { passive: true });
    this.scrollEl.addEventListener('touchcancel', () => (this.touching = false), { passive: true });
    if ('ResizeObserver' in window) {
      this.ro = new ResizeObserver(() => {
        if (this.stuck && !this.touching) this.pin();
      });
      // The conversation as it grows, and the feed as the keyboard, the
      // details or the needs card take room from it.
      this.ro.observe(this.convEl);
      this.ro.observe(this.scrollEl);
    }
    this.bottomEl = h('div', { class: 'bottom session-bottom' });
    this.buildComposer();
    return h('section', { class: 'screen session', 'aria-label': 'Session' }, header, this.detailsEl,
      h('div', { class: 'feed-wrap' }, this.scrollEl, this.pill), this.bottomEl);
  }

  buildComposer() {
    this.input = h('textarea', { rows: '1', enterkeyhint: 'enter', autocapitalize: 'sentences', 'aria-label': 'Message' });
    this.sendBtn = h('button', { class: 'send', 'aria-label': 'Send', html: ICON.up, disabled: true, onclick: () => this.send() });
    this.problemEl = h('div', { class: 'problem', role: 'alert' });
    this.input.addEventListener('input', () => {
      grow(this.input);
      this.sendBtn.disabled = !this.input.value.trim() || this.sending;
      this.problemEl.textContent = '';
    });
    this.input.addEventListener('keydown', (e) => {
      // On a phone Return is a new line and the button sends; with a keyboard, Return sends.
      if (e.key === 'Enter' && !e.shiftKey && (e.metaKey || e.ctrlKey || matchMedia('(pointer: fine)').matches)) {
        e.preventDefault();
        this.send();
      }
    });
    this.composer = h('div', { class: 'composer' }, this.problemEl, h('div', { class: 'field' }, this.input, this.sendBtn));
  }

  // ---- Frames ---------------------------------------------------------------

  onFrame(f) {
    if (f.t === 'conversation' && f.conversation) {
      this.apply(f.conversation);
    } else if ((f.t === 'upsert' && f.session && f.session.id === this.sid)) {
      this.conn.sessions.set(this.sid, f.session);
      this.ctx.changed(this.conn);
    } else if (f.t === 'snapshot' && Array.isArray(f.sessions)) {
      const s = f.sessions.find((x) => x.id === this.sid);
      if (s) {
        this.conn.sessions.set(this.sid, s);
        this.ctx.changed(this.conn);
      }
    }
    this.invalidate();
  }

  apply(u) {
    if (u.session && u.session !== this.sid) return;
    const fresh = u.items || [];
    if (u.reset) {
      const versions = new Map();
      this.items = fresh;
      this.index = new Map();
      fresh.forEach((it, i) => {
        this.index.set(it.id, i);
        const json = JSON.stringify(it);
        const old = this.versions.get(it.id);
        versions.set(it.id, { json, v: old ? (old.json === json ? old.v : old.v + 1) : 0 });
      });
      this.versions = versions;
      this.more = !!u.more;
    } else {
      for (const it of fresh) {
        const json = JSON.stringify(it);
        const old = this.versions.get(it.id);
        if (old && old.json === json) continue;
        this.versions.set(it.id, { json, v: old ? old.v + 1 : 0 });
        const i = this.index.get(it.id);
        if (i !== undefined) this.items[i] = it;
        else {
          this.index.set(it.id, this.items.length);
          this.items.push(it);
        }
      }
    }
    this.needs = u.needs || null;
    this.pending = u.pending || [];
    this.transcript = u.transcript || null;
    this.loaded = true;
    this.problem = '';
  }

  /** The row changed on the list's stream. */
  rowChanged() {
    this.invalidate();
  }

  invalidate() {
    if (this.scheduled) return;
    this.scheduled = true;
    requestAnimationFrame(() => {
      this.scheduled = false;
      this.render();
    });
  }

  // ---- Drawing --------------------------------------------------------------

  render() {
    this.renderHeader();
    this.renderFeed();
    this.renderBottom();
  }

  renderHeader() {
    const s = this.row;
    const name = s ? s.name : this.lastName || 'Session';
    if (s) this.lastName = s.name;
    if (this.nameEl.textContent !== name) this.nameEl.textContent = name;
    const lk = s ? L.look(s) : null;
    const sub = s
      ? `<span class="${L.markClass(s, this.redOnDone())}">${L.glyph(lk)}</span><span class="r-${L.role(lk, this.redOnDone())} st">${esc(L.label(s))}</span><span class="dot">·</span><span>${esc(this.conn.m.name)}</span>`
      : `<span>Closed</span><span class="dot">·</span><span>${esc(this.conn.m.name)}</span>`;
    if (this.subHTML !== sub) {
      this.subHTML = sub;
      this.subEl.innerHTML = sub;
    }
    this.titleBtn.setAttribute('aria-label', s ? `${s.name}, ${L.label(s)}, on ${this.conn.m.name}. Shows the details.` : name);
    if (!this.detailsEl.hidden) this.renderDetails();
  }

  redOnDone() {
    return !!(this.conn.header && this.conn.header.red_on_done);
  }

  toggleDetails() {
    const open = this.detailsEl.hidden;
    this.detailsEl.hidden = !open;
    this.titleBtn.setAttribute('aria-expanded', String(open));
    if (open) this.renderDetails();
  }

  renderDetails() {
    const s = this.row;
    if (!s) {
      this.detailsEl.replaceChildren(h('div', { class: 'hint', text: 'This session was closed.' }));
      return;
    }
    const rows = [];
    const add = (k, v, mono) => {
      if (v == null || v === '') return;
      rows.push(h('div', { class: 'd-k', text: k }), h('div', { class: 'd-v' + (mono ? ' mono-inline' : ''), text: String(v) }));
    };
    add('State', L.label(s) + (s.reason ? ` (${s.reason})` : ''));
    add('Doing', s.detail);
    add('Call', s.command, true);
    if (s.loose) add('Not landed', looseWords(s.loose));
    add('Repo', s.repo, true);
    add('Branch', s.branch, true);
    add(s.worktree ? 'Worktree' : 'Folder', s.worktree || s.cwd, true);
    const agent = [s.profile && '+' + s.profile, s.model].filter(Boolean).join(' · ');
    add('Agent', agent + (s.routed ? ` (auto chose it${s.class ? ' for a ' + s.class + ' task' : ''})` : ''));
    if (s.tokens) {
      const t = s.tokens;
      add('Tokens', `${L.count(L.tokenTotal(t))}: ${L.count(t.input)} in, ${L.count(t.output)} out, ${L.count(t.cache_read)} cache read, ${L.count(t.cache_write)} cache write`);
    }
    if (s.cost_usd != null) add('Cost', '$' + s.cost_usd.toFixed(2) + ' at list price');
    if (s.context != null) add('Context', Math.round(s.context) + '% full');
    if (s.memory) add('Memory', Math.round(s.memory / 1048576) + ' MB');
    if (s.subagents && s.subagents.length) add('Helpers', s.subagents.map((a) => `${a.type || 'Helper'}${a.detail ? ' — ' + a.detail : ''}`).join('\n'));
    if (s.warnings && s.warnings.length) add('Warnings', s.warnings.join('\n'));
    if (s.rated) add('Rated', s.rated);
    const t = L.when(s.created);
    if (t) add('Started', L.clock(t));
    add('Session', s.id, true);
    this.detailsEl.replaceChildren(h('div', { class: 'd-grid' }, rows));
  }

  /** The conversation's blocks: each a message, a reply, a call, a run of calls, a helper, a note or a time. */
  blocks() {
    const out = [];
    const s = this.row;
    let run = [];
    let prev = null;
    const flush = () => {
      if (run.length >= 2) out.push({ key: 'g:' + run[0].id, type: 'group', items: run });
      else for (const it of run) out.push({ key: it.id, type: 'tool', items: [it] });
      run = [];
    };
    for (const it of this.items) {
      const t = L.when(it.time);
      const gap = t != null && (prev == null || t - prev >= GAP);
      const groupable = it.kind === 'tool' && it.group && it.state === 'ok';
      if (!groupable || gap || (run.length && run[0].group !== it.group)) flush();
      if (gap) out.push({ key: 't:' + it.id, type: 'time', t });
      if (t != null) prev = t;
      if (groupable) {
        run.push(it);
        continue;
      }
      if (it.kind === 'user') out.push({ key: it.id, type: 'user', items: [it] });
      else if (it.kind === 'text') out.push({ key: it.id, type: 'text', items: [it] });
      else if (it.kind === 'note') out.push({ key: it.id, type: 'note', items: [it] });
      else if (it.kind === 'agent') {
        const live = liveDetail(it, s);
        const running = it.state === 'running' || !!(it.agent && s && (s.subagents || []).some((a) => a.agent === it.agent));
        out.push({ key: it.id, type: 'agent', items: [it], extra: (running ? 'r' : '') + '|' + (live || ''), live, running });
      } else if (it.kind === 'needs') out.push({ key: it.id, type: 'needitem', items: [it] });
      else out.push({ key: it.id, type: 'tool', items: [it] });
    }
    flush();
    for (const it of this.pending) out.push({ key: 'p:' + it.id, type: 'user', items: [it], pending: true });
    for (const b of out) {
      b.sig = b.type + '|' + (b.items ? b.items.map((it) => it.id + ':' + (b.pending ? JSON.stringify(it) : (this.versions.get(it.id) || { v: 0 }).v)).join(',') : b.t) + '|' + (b.extra || '');
    }
    return out;
  }

  renderFeed() {
    const s = this.row;
    this.moreEl.hidden = !this.more;
    const blocks = this.loaded ? this.blocks() : [];
    const empty = !blocks.length && !this.needs;
    this.emptyEl.hidden = !empty;
    if (empty) this.renderEmpty(s);
    if (!this.drawn && blocks.length) this.stuck = true;
    const stick = this.stuck;
    this.reconcile(blocks);
    const last = blocks.length ? blocks[blocks.length - 1].key : '';
    const grew = last !== this.lastKey;
    this.lastKey = last;
    if (blocks.length && !this.drawn) {
      this.drawn = true;
      requestAnimationFrame(() => this.convEl.setAttribute('aria-live', 'polite'));
    }
    if (stick) this.pin();
    else if (grew && blocks.length) this.pill.hidden = false;
  }

  renderEmpty(s) {
    let title, text = '', term = false;
    if (!this.loaded) {
      if (this.problem) {
        title = 'Can’t follow this conversation';
        text = this.problem;
        term = true;
      } else title = 'Loading the conversation…';
    } else if (s && s.state === 'starting') {
      title = 'Starting…';
      text = 'Its conversation shows here as soon as it begins.';
    } else if (s && s.state === 'stalled') {
      title = 'It hasn’t started';
      text = s.detail ? s.detail + '.' : '';
      term = true;
    } else if (!this.transcript) {
      title = 'This one lives in its terminal';
      text = 'It started before Irie could follow conversations, so there’s nothing to show here. Its terminal has all of it.';
      term = true;
    } else {
      title = 'Nothing here yet';
      text = 'Messages show here as they come.';
    }
    const key = title + '|' + text + '|' + term + '|' + this.showTerm + '|' + (this.screen ? this.screen.lines.length : -1);
    if (this.emptyKey === key) return;
    this.emptyKey = key;
    this.emptyEl.replaceChildren(
      h('div', { class: 'empty-mark', html: L.sun('sun big') }),
      h('h2', { class: 'empty-title', text: title }),
      text ? h('p', { class: 'empty-text', text }) : null,
      term ? h('button', { class: 'btn', onclick: () => this.toggleTerminal() }, h('span', { class: 'btn-ic', html: ICON.terminal }), this.showTerm ? 'Hide the terminal' : 'Show the terminal') : null,
      term && this.showTerm ? this.terminalBlock() : null);
  }

  reconcile(blocks) {
    const list = this.convEl;
    const seen = new Set();
    let cursor = list.firstElementChild;
    for (const b of blocks) {
      seen.add(b.key);
      let el = this.blockEls.get(b.key);
      if (!el || el._sig !== b.sig) {
        const fresh = this.renderBlock(b);
        fresh._sig = b.sig;
        if (el) {
          if (el === cursor) cursor = fresh;
          el.replaceWith(fresh);
        }
        this.blockEls.set(b.key, fresh);
        el = fresh;
      }
      if (el === cursor) cursor = cursor.nextElementSibling;
      else list.insertBefore(el, cursor);
    }
    for (const [k, el] of this.blockEls) {
      if (!seen.has(k)) {
        el.remove();
        this.blockEls.delete(k);
      }
    }
  }

  renderBlock(b) {
    switch (b.type) {
      case 'user': return this.userBlock(b.items[0], b.pending, b.key);
      case 'text': return h('div', { class: 'blk prose', html: md(b.items[0].text) });
      case 'note': return h('div', { class: 'blk note-line', role: 'separator' }, h('span', { text: b.items[0].text || '' }));
      case 'time': return h('div', { class: 'blk time-line', text: L.clock(b.t) });
      case 'group': return this.groupBlock(b);
      case 'agent': return this.agentBlock(b);
      case 'needitem': return this.callBlock(b.items[0], 'needs');
      default: return this.callBlock(b.items[0]);
    }
  }

  userBlock(it, pending, key) {
    const text = it.text || '';
    const long = text.split('\n').length > 12 || text.length > 1400;
    const open = this.expanded.has(key);
    const bubble = h('div', { class: 'bubble' + (long && !open ? ' folded' : ''), text });
    let note = '';
    if (pending) note = it.queued ? 'It reads this at its next step' : 'On its way';
    else if (it.queued) note = 'Sent while it worked';
    if (it.from === 'parent') note = 'From its session';
    const el = h('div', { class: 'blk you' + (pending ? ' pending' : '') },
      bubble,
      long ? h('button', {
        class: 'linkish fold',
        text: open ? 'Show less' : 'Show all',
        onclick: (e) => {
          const now = !this.expanded.has(key);
          if (now) this.expanded.add(key); else this.expanded.delete(key);
          bubble.classList.toggle('folded', !now);
          e.currentTarget.textContent = now ? 'Show less' : 'Show all';
        },
      }) : null,
      note ? h('div', { class: 'you-note' }, pending ? h('span', { class: 'ic', html: ICON.clock }) : null, note) : null);
    return el;
  }

  callBlock(it, kind) {
    const open = this.expanded.has(it.id);
    const failed = it.state === 'error';
    const more = h('div', { class: 'call-more', hidden: !open },
      it.detail ? h('pre', { class: 'mono', text: it.detail }) : null,
      it.output ? h('pre', { class: 'mono err', text: it.output }) : null,
      !it.detail && !it.output ? h('div', { class: 'hint', text: it.tool || 'No details' }) : null);
    const phrase = kind === 'needs' ? it.phrase || it.text || 'Needs you' : it.phrase || it.tool || 'A tool call';
    const btn = h('button', { class: 'call' + (failed ? ' err' : '') + (kind === 'needs' ? ' needs' : ''), 'aria-expanded': String(open) },
      kind === 'needs' ? h('span', { class: 'cm mark r-needs', html: L.glyph('needs') }) : callMark(it.state),
      h('span', { class: 'ph', text: phrase }),
      changes(it.added, it.removed),
      it.ms > 0 ? h('span', { class: 'ms', text: L.took(it.ms) }) : null,
      h('span', { class: 'chev', html: ICON.chevron }));
    btn.setAttribute('aria-label', `${phrase}, ${callWord(kind === 'needs' ? 'needs' : it.state)}`);
    btn.addEventListener('click', () => this.flip(it.id, btn, more));
    return h('div', { class: 'blk callblk' }, btn, more);
  }

  groupBlock(b) {
    const items = b.items;
    const open = this.expanded.has(b.key);
    const files = new Set(items.filter((i) => i.detail).map((i) => L.base(i.detail)));
    const one = files.size === 1 ? [...files][0] : null;
    const kind = items[0].group;
    let phrase;
    if (kind === 'edit') phrase = one ? `Edited ${one} ${items.length} times` : `Edited ${files.size} files`;
    else if (kind === 'read') phrase = one ? `Read ${one} ${items.length} times` : `Read ${files.size} files`;
    else phrase = `Searched ${items.length} times`;
    const added = items.reduce((n, i) => n + (i.added || 0), 0);
    const removed = items.reduce((n, i) => n + (i.removed || 0), 0);
    const ms = items.reduce((n, i) => n + (i.ms || 0), 0);
    const more = h('div', { class: 'call-more group-more', hidden: !open }, items.map((it) => this.subCall(it)));
    const btn = h('button', { class: 'call', 'aria-expanded': String(open), 'aria-label': `${phrase}, done` },
      callMark('ok'), h('span', { class: 'ph', text: phrase }), changes(added, removed),
      ms > 0 ? h('span', { class: 'ms', text: L.took(ms) }) : null, h('span', { class: 'chev', html: ICON.chevron }));
    btn.addEventListener('click', () => this.flip(b.key, btn, more));
    return h('div', { class: 'blk callblk' }, btn, more);
  }

  subCall(it) {
    const open = this.expanded.has(it.id);
    const more = h('div', { class: 'sub-more', hidden: !open }, it.detail ? h('pre', { class: 'mono', text: it.detail }) : h('div', { class: 'hint', text: it.tool || 'No details' }));
    const btn = h('button', { class: 'subcall', 'aria-expanded': String(open) },
      callMark(it.state), h('span', { class: 'ph', text: it.phrase || it.tool || 'A tool call' }), changes(it.added, it.removed),
      it.ms > 0 ? h('span', { class: 'ms', text: L.took(it.ms) }) : null);
    btn.addEventListener('click', () => this.flip(it.id, btn, more));
    return h('div', { class: 'subwrap' }, btn, more);
  }

  agentBlock(b) {
    const it = b.items[0];
    const open = this.expanded.has(it.id);
    const state = b.running ? 'running' : it.state;
    const more = h('div', { class: 'call-more', hidden: !open },
      it.tools > 0 ? h('div', { class: 'hint', text: it.tools === 1 ? '1 tool call' : `${it.tools} tool calls` }) : null,
      it.detail ? h('pre', { class: 'mono', text: it.detail }) : null);
    const btn = h('button', { class: 'call helper', 'aria-expanded': String(open), 'aria-label': `Helper ${it.agent_type || ''}: ${it.text || it.phrase || ''}, ${callWord(state)}` },
      h('span', { class: 'arrow', text: '↳' }),
      h('span', { class: 'ph' }, h('b', { text: it.agent_type || 'Helper' }), h('span', { class: 'task', text: ' — ' + (it.text || it.phrase || '') })),
      callMark(state),
      it.ms > 0 && !b.running ? h('span', { class: 'ms', text: L.took(it.ms) }) : null);
    btn.addEventListener('click', () => this.flip(it.id, btn, more));
    return h('div', { class: 'blk callblk' }, btn, b.live && b.running ? h('div', { class: 'live', text: b.live }) : null, more);
  }

  flip(key, btn, more) {
    const open = more.hidden;
    more.hidden = !open;
    btn.setAttribute('aria-expanded', String(open));
    if (open) this.expanded.add(key);
    else this.expanded.delete(key);
  }

  nearBottom() {
    const sc = this.scrollEl;
    return sc.scrollHeight - sc.scrollTop - sc.clientHeight < 80;
  }

  /** Holds the newest item in view. */
  pin() {
    this.scrollEl.scrollTop = this.scrollEl.scrollHeight;
  }

  toBottom(smooth) {
    this.pill.hidden = true;
    if (smooth && !reduced()) this.scrollEl.scrollTo({ top: this.scrollEl.scrollHeight, behavior: 'smooth' });
    else {
      this.stuck = true;
      this.pin();
    }
  }

  // ---- The foot: message box, needs card, or why not ------------------------

  renderBottom() {
    const s = this.row;
    let mode;
    if (!s) mode = 'gone';
    else if (this.needs || s.state === 'needs_you') mode = 'needs';
    else if (s.state === 'exited' || s.state === 'failed') mode = 'ended';
    else if (s.state === 'starting' || s.state === 'stalled') mode = 'early';
    else if (s.state === 'frozen' || s.frozen) mode = 'frozen';
    else mode = 'compose';
    const changed = mode !== this.mode;
    this.mode = mode;
    if (mode === 'compose') {
      if (changed) {
        this.bottomEl.replaceChildren(this.composer);
        this.bottomEl.className = 'bottom session-bottom';
      }
      const p = `Message ${s.name}…`;
      if (this.input.placeholder !== p) this.input.placeholder = p;
      this.input.setAttribute('aria-label', `Message ${s.name}`);
      this.stopPolling();
      return;
    }
    if (mode === 'needs') {
      this.bottomEl.className = 'bottom session-bottom needs-bottom';
      if (changed) {
        this.screen = null;
        this.screenProblem = '';
        this.showTerm = false;
        this.showKeys = false;
        this.needsKey = '';
        this.poll(true);
      }
      this.renderNeeds();
      return;
    }
    this.stopPolling();
    this.bottomEl.className = 'bottom session-bottom';
    let text, button = null;
    if (mode === 'gone') {
      text = 'This session was closed.';
      button = h('button', { class: 'btn primary', text: 'Reopen', onclick: () => this.ctx.reopen(this.conn, this.sid) });
    } else if (mode === 'ended') {
      text = s.state === 'failed' ? (s.detail ? `It failed: ${s.detail}` : 'It failed to start.') : (s.exit_code ? `It ended with an error (exit code ${s.exit_code}).` : 'It has ended.');
    } else if (mode === 'early') {
      text = s.state === 'stalled' ? (s.detail ? `It hasn’t started: ${s.detail}` : 'It hasn’t started.') : 'Starting… It takes messages once it’s ready.';
    } else {
      text = 'Frozen. Thaw it to send a message.';
      button = h('button', { class: 'btn primary', text: 'Thaw', onclick: () => this.ctx.op(this.conn, 'thaw', { session: this.sid }) });
    }
    const key = mode + '|' + text;
    if (this.noteKey === key && !changed) return;
    this.noteKey = key;
    this.bottomEl.replaceChildren(h('div', { class: 'composer-note' + (mode === 'ended' && s.state === 'failed' ? ' loud' : '') }, h('span', { text }), button));
  }

  renderNeeds() {
    const s = this.row;
    const n = this.needs || {};
    const title = n.phrase || n.text || (s && s.detail) || 'It needs you';
    const detail = n.detail || (s && !this.needs ? s.command : '') || '';
    const key = JSON.stringify([title, detail, n.options, this.screen, this.screenProblem, this.showTerm, this.showKeys, this.busyKey]);
    if (key === this.needsKey) return;
    this.needsKey = key;
    const plan = detail && (n.tool === 'ExitPlanMode' || (detail.includes('\n') && /(^|\n)(#|[-*] |\d+\. )/.test(detail)));
    const opts = this.screen && this.screen.options && this.screen.options.length ? this.screen.options : null;
    const parts = [
      h('div', { class: 'needs-title' }, h('span', { class: 'mark r-needs', html: L.glyph('needs') }), h('span', { text: title })),
    ];
    if (detail) parts.push(plan ? h('div', { class: 'needs-detail prose', html: md(detail) }) : h('pre', { class: 'needs-detail mono', text: detail }));
    if (!opts && n.options && n.options.length) {
      parts.push(h('ul', { class: 'needs-choices' }, n.options.map((o) => h('li', { text: o }))));
    }
    if (opts) {
      parts.push(h('div', { class: 'opts', role: 'group', 'aria-label': 'Answer' }, opts.map((o) => h('button', {
        class: 'opt' + (o.selected ? ' sel' : '') + (this.busyKey === o.key ? ' busy' : ''),
        disabled: this.busyKey != null,
        onclick: () => this.press([o.key]),
      }, h('span', { class: 'k', text: o.key }), h('span', { class: 'l', text: o.label })))));
    } else if (!this.screen && !this.screenProblem) {
      parts.push(h('div', { class: 'hint reading' }, h('span', { class: 'ic', html: ICON.ring }), 'Reading its screen…'));
    }
    if (!opts || this.showKeys) parts.push(this.keypad());
    if (this.screenProblem) parts.push(h('div', { class: 'hint', text: this.screenProblem }));
    parts.push(h('div', { class: 'needs-foot' },
      h('button', { class: 'linkish', onclick: () => this.toggleTerminal() }, h('span', { class: 'ic', html: ICON.terminal }), this.showTerm ? 'Hide the terminal' : 'Show the terminal'),
      opts ? h('button', { class: 'linkish', text: this.showKeys ? 'Hide keys' : 'Keys', onclick: () => { this.showKeys = !this.showKeys; this.renderNeeds(); } }) : null));
    if (this.showTerm) parts.push(this.terminalBlock());
    const card = h('div', { class: 'needs-card', role: 'region', 'aria-label': 'It needs you' }, parts);
    this.bottomEl.replaceChildren(card);
    const term = card.querySelector('.term');
    if (term) term.scrollTop = term.scrollHeight;
  }

  keypad() {
    const keys = [['1', '1'], ['2', '2'], ['3', '3'], ['Esc', 'Escape'], ['↑', 'Up'], ['↓', 'Down'], ['Tab', 'Tab'], ['Enter', 'Enter']];
    return h('div', { class: 'keypad', role: 'group', 'aria-label': 'Keys' }, keys.map(([label, key]) => h('button', {
      class: 'key' + (this.busyKey === key ? ' busy' : ''),
      text: label,
      'aria-label': { Escape: 'Escape', Up: 'Up arrow', Down: 'Down arrow', Enter: 'Return' }[key] || label,
      disabled: this.busyKey != null,
      onclick: () => this.press([key]),
    })));
  }

  terminalBlock() {
    if (!this.screen) return h('pre', { class: 'term', text: this.screenProblem || 'Reading its screen…' });
    return h('pre', { class: 'term', 'aria-label': 'Its terminal', text: (this.screen.lines || []).join('\n') || ' ' });
  }

  toggleTerminal() {
    this.showTerm = !this.showTerm;
    if (this.showTerm) this.poll(true);
    this.needsKey = '';
    this.emptyKey = '';
    this.invalidate();
  }

  /** Whether the agent's screen is on show: the needs card, or its terminal. */
  watching() {
    return !this.destroyed && (this.mode === 'needs' || (this.showTerm && !this.emptyEl.hidden));
  }

  /** Reads the agent's screen now and every 1.5 s while it is on show. */
  poll(now) {
    clearTimeout(this.pollTimer);
    this.pollTimer = null;
    const gen = (this.pollGen = (this.pollGen || 0) + 1);
    const go = async () => {
      if (document.visibilityState === 'visible') {
        try {
          const r = await call(this.conn.m, 'screen', { session: this.sid }, { timeout: 5000 });
          this.screen = { lines: r.lines || [], options: r.options || [] };
          this.screenProblem = '';
        } catch (e) {
          if (e.status === 401) return this.ctx.unpaired(this.conn, e.message);
          this.screenProblem = e.code === 'unknown_op' ? 'This machine’s Irie can’t read the screen yet: use the keys.' : e.message;
        }
        if (gen !== this.pollGen) return;
        this.needsKey = '';
        this.emptyKey = '';
        this.invalidate();
      }
      if (gen === this.pollGen && this.watching()) this.pollTimer = setTimeout(go, 1500);
    };
    if (now) go();
    else this.pollTimer = setTimeout(go, 1500);
  }

  stopPolling() {
    if (this.showTerm && !this.emptyEl.hidden) return;
    clearTimeout(this.pollTimer);
    this.pollTimer = null;
    this.pollGen = (this.pollGen || 0) + 1;
  }

  async press(keys) {
    this.busyKey = keys[0];
    this.needsKey = '';
    this.renderNeeds();
    try {
      await call(this.conn.m, 'keys', { session: this.sid, keys }, { timeout: 8000 });
    } catch (e) {
      this.ctx.fail(this.conn, e);
    }
    this.busyKey = null;
    this.needsKey = '';
    await this.poll(true);
  }

  async send() {
    const text = this.input.value.trim();
    if (!text || this.sending) return;
    this.sending = true;
    this.sendBtn.disabled = true;
    this.sendBtn.classList.add('busy');
    this.problemEl.textContent = '';
    try {
      await call(this.conn.m, 'send', { session: this.sid, text }, { timeout: 20000 });
      if (this.input.value.trim() === text) {
        this.input.value = '';
        grow(this.input);
      }
    } catch (e) {
      if (e.status === 401) this.ctx.unpaired(this.conn, e.message);
      else this.problemEl.textContent = e.message;
    } finally {
      this.sending = false;
      this.sendBtn.classList.remove('busy');
      this.sendBtn.disabled = !this.input.value.trim();
    }
    this.toBottom(true);
  }

  openActions() {
    const s = this.row;
    if (!s) return;
    actions({ title: s.name, message: `${L.label(s)} · ${this.conn.m.name}`, items: this.ctx.sessionActions(this.conn, s, { inSession: true }) });
  }
}

// ---- Pieces -------------------------------------------------------------------

function callMark(state) {
  const cls = { ok: 'ok', error: 'err', stopped: 'stop' }[state] || 'run';
  return h('span', { class: 'cm ' + cls, html: ICON[cls === 'run' ? 'ring' : cls] });
}

function callWord(state) {
  return { ok: 'done', error: 'failed', stopped: 'stopped', needs: 'needs you' }[state] || 'running';
}

function changes(added, removed) {
  if (!(added > 0) && !(removed > 0)) return null;
  return h('span', { class: 'chg', 'aria-label': `${added || 0} lines added, ${removed || 0} removed` },
    added > 0 ? h('span', { class: 'ln-add', text: '+' + added }) : null,
    removed > 0 ? h('span', { class: 'ln-rem', text: '−' + removed }) : null);
}

/** What a running helper does now, from the row: its own, or the one of its kind when only one runs. */
function liveDetail(it, s) {
  const subs = (s && s.subagents) || [];
  if (it.agent) {
    const own = subs.find((a) => a.agent === it.agent);
    if (own) return own.detail || '';
  }
  if (it.state !== 'running' || !it.agent_type) return '';
  const same = subs.filter((a) => a.type === it.agent_type);
  return same.length === 1 ? same[0].detail || '' : '';
}

export function looseWords(l) {
  const n = (k, w) => `${k} ${w}${k === 1 ? '' : 's'}`;
  const parts = [];
  if (l.dirty > 0) parts.push(n(l.dirty, 'file') + ' not committed');
  if (l.ahead > 0) parts.push(n(l.ahead, 'commit') + ' not in ' + (l.land || 'its branch'));
  return parts.join(' · ');
}

/** A text box as tall as its text, up to about six lines. */
export function grow(t) {
  t.style.height = 'auto';
  t.style.height = Math.min(t.scrollHeight, 150) + 'px';
}

const reduced = () => matchMedia('(prefers-reduced-motion: reduce)').matches;
