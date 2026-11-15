// Settings: every preference the core keeps, in one dialog, as the Mac
// app's Settings window has them. Each control reads and writes the core's
// own keys through the config op; the arch agents and the plugins are drawn
// as the core lists them, knowing none of them by name (docs/CORE.md §2,
// 7 and 8).

import { h, ICON } from '../shared/ui.js';
import { dialog, confirm, picker } from './menu.js';

/** Puts kids in el in place of what it holds, leaving out null and false, as h() does. */
const fill = (el, ...kids) => el.replaceChildren(...kids.flat(Infinity).filter((k) => k != null && k !== false));

const THEMES = [
  ['rosewater', 'Rosewater'],
  ['evergreen', 'Evergreen'],
  ['terracotta', 'Terracotta'],
  ['lilac', 'Lilac'],
];

const get = (tree, key) => key.split('.').reduce((t, k) => (t == null ? undefined : t[k]), tree);

export function openSettings(ctx) {
  const tabs = [['general', 'General'], ['look', 'Appearance'], ['arch', 'Arch agents'], ['plugins', 'Plugins'], ['about', 'About']];
  let tab = 'general';
  const bar = h('div', { class: 'set-tabs', role: 'tablist' });
  const body = h('div', { class: 'set-body' });
  const d = dialog({ title: 'Settings', wide: true, content: h('div', { class: 'settings' }, bar, body), buttons: [{ label: 'Done', primary: true }] });
  const show = (t) => {
    tab = t;
    fill(bar, ...tabs.map(([id, label]) => h('button', { role: 'tab', 'aria-selected': String(id === tab), text: label, onclick: () => show(id) })));
    fill(body, h('div', { class: 'set-hint' }, h('span', { class: 'spin-ring', html: ICON.ring }), ' Reading…'));
    const draw = { general, look, arch, plugins, about }[t];
    draw(ctx, body).catch((e) => fill(body, h('p', { class: 'set-problem', text: e.message })));
  };
  show(tab);
  return d;
}

async function readConfig(ctx) {
  return ctx.call('config', {}, { timeout: 10000 });
}

async function setKey(ctx, key, value) {
  try {
    await ctx.call('config', { set: [{ key, value }] }, { timeout: 10000 });
    ctx.notice('Saved.', { ms: 1500 });
    return true;
  } catch (e) {
    ctx.notice(e.message, { error: true });
    return false;
  }
}

function toggle(label, help, on, change) {
  const input = h('input', { type: 'checkbox', class: 'switch', checked: !!on, 'aria-label': label });
  input.addEventListener('change', async () => {
    input.disabled = true;
    if (!(await change(input.checked))) input.checked = !input.checked;
    input.disabled = false;
  });
  return h('label', { class: 'set-row' }, h('div', { class: 'set-text' }, h('div', { class: 'set-label', text: label }), help ? h('div', { class: 'set-help', text: help }) : null), input);
}

function choice(label, help, value, options, change) {
  const sel = h('select', { class: 'set-select', 'aria-label': label }, ...options.map(([v, l]) => h('option', { value: v, text: l, selected: v === value })));
  sel.addEventListener('change', () => change(sel.value));
  return h('label', { class: 'set-row' }, h('div', { class: 'set-text' }, h('div', { class: 'set-label', text: label }), help ? h('div', { class: 'set-help', text: help }) : null), sel);
}

async function general(ctx, body) {
  const r = await readConfig(ctx);
  const c = r.config || {};
  const prof = get(c, 'spawn.default_profile') || '';
  const profBtn = h('button', { class: 'btn small', text: prof ? '+' + prof : 'The default' });
  profBtn.addEventListener('click', () => picker({
    title: 'Default agent', current: prof,
    load: async () => {
      const x = await ctx.call('complete', { text: '+' }, { timeout: 8000 });
      const notes = x.notes || [];
      return (x.items || []).map((i, k) => ({ value: i.replace(/^\+/, ''), label: i, note: notes[k] || '' }));
    },
    choose: async (v) => {
      if (await setKey(ctx, 'spawn.default_profile', v)) profBtn.textContent = '+' + v;
    },
  }));
  fill(body, 
    r.problem ? h('p', { class: 'set-problem', text: 'The config file does not read: ' + r.problem }) : null,
    h('div', { class: 'set-row' }, h('div', { class: 'set-text' }, h('div', { class: 'set-label', text: 'Default agent' }), h('div', { class: 'set-help', text: 'What a task runs on when it names no +agent.' })), profBtn),
    toggle('Close finished sessions', 'A session closes once its turn ends and it stays quiet; history keeps it, and Reopen brings it back.', get(c, 'spawn.close_when_done'), (v) => setKey(ctx, 'spawn.close_when_done', v)),
    choice('New sessions go', 'Where a new card joins the list.', get(c, 'ui.new_tabs') || 'last', [['last', 'Last'], ['first', 'First, under Pinned'], ['next', 'Next to the one that started it']], (v) => setKey(ctx, 'ui.new_tabs', v)),
    toggle('Draw finished sessions in red', 'As if they need you, until you look.', get(c, 'ui.red_on_done'), (v) => setKey(ctx, 'ui.red_on_done', v)),
    h('p', { class: 'set-foot', text: 'Every setting lives in ' + (r.path || 'config.toml') + ', inside Linux.' }));
}

async function look(ctx, body) {
  const r = await readConfig(ctx);
  const current = (ctx.header().theme && ctx.header().theme.name) || get(r.config || {}, 'ui.theme') || 'rosewater';
  const grid = h('div', { class: 'themes' });
  const draw = (cur) => fill(grid, ...THEMES.map(([id, name]) => h('button', {
    class: 'theme-pick' + (id === cur ? ' on' : ''), 'aria-pressed': String(id === cur),
    onclick: async () => {
      try {
        await ctx.call('theme', { theme: id }, { timeout: 10000 });
        await ctx.call('config', { set: [{ key: 'ui.theme', value: id }] }, { timeout: 10000 });
        try {
          localStorage.setItem('irie.theme', JSON.stringify(id));
        } catch {}
        draw(id);
      } catch (e) {
        ctx.notice(e.message, { error: true });
      }
    },
  }, h('span', { class: 'theme-swatch t-' + id }), h('span', { text: name }))));
  draw(current);
  fill(body, 
    h('div', { class: 'set-row col' }, h('div', { class: 'set-text' }, h('div', { class: 'set-label', text: 'Palette' }),
      h('div', { class: 'set-help', text: 'The cards’, the window’s and Irie’s status line in the terminal. Light or dark follows Windows.' })), grid),
    h('p', { class: 'set-foot', text: 'Ctrl+= and Ctrl+- make the terminal’s text bigger or smaller; Ctrl+0 puts it back.' }));
}

async function arch(ctx, body) {
  const r = await ctx.call('arch', {}, { timeout: 10000 });
  const agents = r.agents || [];
  if (!agents.length) {
    fill(body, h('p', { class: 'set-hint', text: 'This Irie has no arch agents.' }));
    return;
  }
  const redraw = () => arch(ctx, body).catch(() => {});
  fill(body, ...agents.map((a) => h('section', { class: 'arch' + (a.on ? '' : ' off') },
    h('div', { class: 'arch-head' }, h('span', { class: 'arch-icon', text: a.icon || '' }),
      h('div', { class: 'arch-names' }, h('b', { text: cap(a.name) + (a.nickname ? ` (${a.nickname})` : '') }), h('span', { class: 'arch-title', text: a.title || '' })),
      h('span', { class: 'arch-on', text: a.on ? 'On' : 'Off' })),
    a.about ? h('p', { class: 'arch-about', text: a.about }) : null,
    a.status ? h('p', { class: 'arch-status', text: a.status }) : null,
    (a.items || []).length ? h('ul', { class: 'arch-items' }, ...a.items.map((it) => h('li', { class: 'arch-item ' + (it.level || 'note') },
      h('div', { class: 'arch-item-text', text: it.text }), it.detail ? h('div', { class: 'arch-item-detail', text: it.detail }) : null,
      actions(ctx, it.actions, redraw)))) : null,
    actions(ctx, a.actions, redraw),
    ...(a.settings || []).map((s) => setting(ctx, s)))));
}

function actions(ctx, list, redraw) {
  if (!list || !list.length) return null;
  return h('div', { class: 'arch-acts' }, ...list.map((act) => h('button', {
    class: 'btn small', text: act.label,
    onclick: async () => {
      // One with ask is the person's to OK first; the op refuses it without (CORE.md §2.7).
      if (act.ask && !(await confirm({ title: act.label, message: act.ask, ok: act.label }))) return;
      const args = { ...(act.args || {}) };
      if (act.ask) args.ok = true;
      try {
        await ctx.call(act.op, args, { timeout: 60000 });
        if (act.done) ctx.notice(act.done);
        redraw();
      } catch (e) {
        ctx.notice(e.message, { error: true });
      }
    },
  })));
}

function setting(ctx, s) {
  const value = s.value;
  switch (s.kind) {
    case 'bool':
      return toggle(s.label, s.help, !!value, (v) => setKey(ctx, s.key, v));
    case 'duration':
    case 'number': {
      const input = h('input', { class: 'set-input', type: s.kind === 'number' ? 'number' : 'text', value: value == null ? '' : String(value), 'aria-label': s.label });
      input.addEventListener('change', () => setKey(ctx, s.key, s.kind === 'number' ? Number(input.value) : input.value.trim()));
      return h('label', { class: 'set-row' }, h('div', { class: 'set-text' }, h('div', { class: 'set-label', text: s.label }), s.help ? h('div', { class: 'set-help', text: s.help }) : null), input);
    }
    case 'lines': {
      const ta = h('textarea', { class: 'set-input lines', rows: '3', 'aria-label': s.label });
      ta.value = Array.isArray(value) ? value.join('\n') : '';
      ta.addEventListener('change', () => setKey(ctx, s.key, ta.value.split('\n').map((l) => l.trim()).filter(Boolean)));
      return h('label', { class: 'set-row col' }, h('div', { class: 'set-text' }, h('div', { class: 'set-label', text: s.label }), s.help ? h('div', { class: 'set-help', text: s.help }) : null), ta);
    }
  }
  return null;
}

async function plugins(ctx, body) {
  const r = await ctx.call('plugins', {}, { timeout: 10000 });
  const redraw = () => plugins(ctx, body).catch(() => {});
  const turn = async (args) => {
    try {
      await ctx.call('plugins', args, { timeout: 15000 });
      return true;
    } catch (e) {
      ctx.notice(e.message, { error: true });
      return false;
    }
  };
  const MARK = { ready: '●', needs: '!', off: '·', plugin_off: '·', held: '○', missing: '○' };
  const WORDS = { ready: 'Ready', needs: 'Needs setting up', off: 'Off', plugin_off: 'Plugin off', held: 'Not in Irie yet', missing: 'Missing' };
  fill(body, 
    ...(r.bundles || []).map((b) => h('section', { class: 'plugin bundle' },
      h('div', { class: 'plugin-head' }, h('div', { class: 'set-text' }, h('b', { text: b.title }), h('div', { class: 'set-help', text: b.summary || '' })),
        bundleSwitch(b, (on) => turn({ bundle: b.name, on }).then((ok) => (ok && redraw(), ok)))),
      b.problem ? h('p', { class: 'set-problem', text: b.problem }) : null)),
    ...(r.plugins || []).map((p) => h('section', { class: 'plugin' + (p.on ? '' : ' off') },
      h('div', { class: 'plugin-head' }, h('div', { class: 'set-text' }, h('b', { text: p.title }), h('div', { class: 'set-help', text: p.summary || '' })),
        p.problem ? null : sw(p.title, p.on, (on) => turn({ plugin: p.name, on }).then((ok) => (ok && redraw(), ok)))),
      p.problem ? h('p', { class: 'set-problem', text: p.problem }) : null,
      h('ul', { class: 'parts' }, ...(p.parts || []).map((part) => h('li', { class: 'part s-' + part.state },
        h('span', { class: 'part-mark', title: WORDS[part.state] || part.state, text: MARK[part.state] || '·' }),
        h('div', { class: 'set-text' }, h('div', { class: 'set-label', text: part.title }), h('div', { class: 'set-help', text: part.detail || part.summary || '' })),
        part.setup && part.state === 'needs' ? h('button', { class: 'btn small', text: 'Set up…', onclick: () => ctx.input(part.setup) }) : null,
        part.switch ? sw(part.title, part.on, (on) => turn({ plugin: p.name, part: part.name, on }).then((ok) => (ok && redraw(), ok)), !p.on) : null))),
      p.setup && p.parts && p.parts.some((x) => x.state === 'needs') ? h('button', { class: 'btn small', text: 'Set up each part…', onclick: () => ctx.input(p.setup) }) : null)));
}

function sw(label, on, change, disabled = false) {
  const input = h('input', { type: 'checkbox', class: 'switch', checked: !!on, disabled, 'aria-label': label });
  input.addEventListener('change', async () => {
    input.disabled = true;
    if (!(await change(input.checked))) input.checked = !input.checked;
    input.disabled = false;
  });
  return input;
}

function bundleSwitch(b, change) {
  const input = sw(b.title, b.state === 'on', change);
  input.indeterminate = b.state === 'some';
  return input;
}

async function about(ctx, body) {
  const l = ctx.link || {};
  fill(body, 
    h('div', { class: 'about' },
      h('p', {}, h('b', { text: 'Irie for Windows ' }), h('span', { text: l.app || '' })),
      h('p', { text: `Irie ${l.version || ctx.version || ''} inside ${l.distro || 'Linux'} (WSL 2) runs the agents; this window draws them.` }),
      h('p', {}, h('button', { class: 'linkish', text: 'Irie’s guide', onclick: () => ctx.open('https://irie-people.github.io/irie/') }), ' · ',
        h('button', { class: 'linkish', text: 'Irie on Windows', onclick: () => ctx.open('https://github.com/irie-people/irie/blob/main/docs/WINDOWS.md') }), ' · ',
        h('button', { class: 'linkish', text: 'Source', onclick: () => ctx.open('https://github.com/irie-people/irie') })),
      h('p', { class: 'set-help', text: 'Irie is free software under the Apache License 2.0. The terminal is drawn by xterm.js (MIT), which the app carries with its license; WebView2 is Microsoft’s, from Windows.' }),
      h('p', {}, h('button', { class: 'btn small', text: 'Licenses of what it carries', onclick: () => notices() }))));
}

async function notices() {
  let text = '';
  try {
    text = await (await fetch('vendor/NOTICES.txt')).text();
  } catch (e) {
    text = e.message;
  }
  dialog({ title: 'Licenses', wide: true, content: h('pre', { class: 'notices-text', text }), buttons: [{ label: 'Done', primary: true }] });
}

const cap = (s) => (s ? s[0].toUpperCase() + s.slice(1) : '');
