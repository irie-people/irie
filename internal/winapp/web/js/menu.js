// The desktop's small pieces: menus at the pointer or under a button,
// dialogs that ask for a name or an OK, pickers, and notices. All keyboard
// friendly: arrows and Return in a menu, Esc closes, and the keyboard goes
// back where it was.

import { h } from '../shared/ui.js';

let open = null;

/** Closes the menu or dialog on show, if any. */
export function closeAll() {
  if (open) open.close();
}

export const isOpen = () => !!open;

/**
 * A menu of items at x, y: {label, run, key, danger, disabled, checked} or
 * '-' for a line. anchor, when given, is the element it hangs from.
 */
export function menu(items, { x, y, anchor, label = 'Menu' } = {}) {
  closeAll();
  const back = document.activeElement;
  const list = h('div', { class: 'menu', role: 'menu', 'aria-label': label, tabindex: '-1' });
  const buttons = [];
  for (const it of items) {
    if (it === '-') {
      list.append(h('div', { class: 'menu-line', role: 'separator' }));
      continue;
    }
    if (!it) continue;
    const b = h('button', {
      class: 'menu-item' + (it.danger ? ' danger' : ''), role: it.checked == null ? 'menuitem' : 'menuitemcheckbox',
      'aria-checked': it.checked == null ? null : String(!!it.checked), disabled: !!it.disabled,
      onclick: () => {
        done();
        it.run && it.run();
      },
    }, h('span', { class: 'menu-check', text: it.checked ? '✓' : '' }), h('span', { class: 'menu-label', text: it.label }),
    it.key ? h('span', { class: 'menu-key', text: it.key }) : null);
    buttons.push(b);
    list.append(b);
  }
  const scrim = h('div', { class: 'scrim clear', onmousedown: (e) => { if (e.target === scrim) done(); } }, list);
  document.body.append(scrim);
  // Where it goes: under the anchor, or at the pointer, kept on screen.
  const r = anchor ? anchor.getBoundingClientRect() : null;
  let left = r ? r.left : x, top = r ? r.bottom + 4 : y;
  const w = list.offsetWidth, ht = list.offsetHeight;
  if (left + w > innerWidth - 8) left = Math.max(8, (r ? r.right : innerWidth) - w);
  if (top + ht > innerHeight - 8) top = Math.max(8, (r ? r.top - ht - 4 : innerHeight - ht - 8));
  list.style.left = left + 'px';
  list.style.top = top + 'px';
  const move = (d) => {
    const enabled = buttons.filter((b) => !b.disabled);
    if (!enabled.length) return;
    const i = enabled.indexOf(document.activeElement);
    enabled[(i + d + enabled.length) % enabled.length].focus();
  };
  list.addEventListener('keydown', (e) => {
    if (e.key === 'ArrowDown') { e.preventDefault(); move(1); }
    else if (e.key === 'ArrowUp') { e.preventDefault(); move(-1); }
    else if (e.key === 'Escape') { e.preventDefault(); done(); }
    else if (e.key === 'Tab') { e.preventDefault(); move(e.shiftKey ? -1 : 1); }
  });
  function done() {
    if (open !== me) return;
    open = null;
    scrim.remove();
    if (back && back.focus) back.focus({ preventScroll: true });
  }
  const me = { close: done };
  open = me;
  (buttons.find((b) => !b.disabled) || list).focus({ preventScroll: true });
  return me;
}

/** A dialog: a title, a message, content, and buttons; Esc is the first that cancels. */
export function dialog({ title, message, content, buttons = [], onClose, wide = false }) {
  closeAll();
  const back = document.activeElement;
  const foot = h('div', { class: 'dialog-foot' });
  const box = h('div', { class: 'dialog' + (wide ? ' wide' : ''), role: 'dialog', 'aria-modal': 'true', 'aria-label': title },
    title ? h('h2', { class: 'dialog-title', text: title }) : null,
    message ? h('p', { class: 'dialog-message', text: message }) : null,
    content || null, foot);
  const scrim = h('div', { class: 'scrim' }, box);
  const me = {
    close(result) {
      if (open !== me) return;
      open = null;
      scrim.remove();
      onClose && onClose(result);
      if (back && back.focus) back.focus({ preventScroll: true });
    },
    box,
  };
  for (const b of buttons) {
    foot.append(h('button', {
      class: 'btn' + (b.primary ? ' primary' : '') + (b.danger ? ' danger' : ''),
      text: b.label,
      onclick: async () => {
        if (b.run && (await b.run()) === false) return;
        me.close(b.value);
      },
    }));
  }
  box.addEventListener('keydown', (e) => {
    if (e.key === 'Escape') {
      e.preventDefault();
      me.close();
    }
  });
  scrim.addEventListener('mousedown', (e) => {
    if (e.target === scrim) me.close();
  });
  document.body.append(scrim);
  open = me;
  const first = box.querySelector('input, textarea, select') || foot.querySelector('.primary') || foot.querySelector('button');
  if (first) first.focus();
  return me;
}

/** Asks for a line of text; run(value) may refuse it by throwing, whose words show. */
export function ask({ title, label, value = '', placeholder = '', ok = 'Save', run }) {
  const input = h('input', { class: 'field-in', type: 'text', value, placeholder, 'aria-label': label || title, spellcheck: 'false' });
  const problem = h('p', { class: 'dialog-problem', hidden: true });
  const submit = async () => {
    const v = input.value.trim();
    if (!v) return false;
    try {
      await run(v);
      return true;
    } catch (e) {
      problem.hidden = false;
      problem.textContent = e.message;
      return false;
    }
  };
  const d = dialog({
    title,
    content: h('div', { class: 'dialog-body' }, label ? h('label', { class: 'dialog-label', text: label }) : null, input, problem),
    buttons: [{ label: 'Cancel' }, { label: ok, primary: true, run: submit }],
  });
  input.select();
  input.addEventListener('keydown', async (e) => {
    if (e.key === 'Enter') {
      e.preventDefault();
      if (await submit()) d.close(true);
    }
  });
  return d;
}

/** Asks OK first: resolves true on OK. */
export function confirm({ title, message, ok = 'OK', danger = false }) {
  return new Promise((resolve) => {
    dialog({
      title, message,
      buttons: [{ label: 'Cancel', value: false }, { label: ok, primary: !danger, danger, value: true }],
      onClose: (v) => resolve(!!v),
    });
  });
}

/** A list to pick from, with a filter: options are {value, label, note}. */
export function picker({ title, load, current, choose }) {
  const filter = h('input', { class: 'field-in', type: 'search', placeholder: 'Filter', 'aria-label': 'Filter', spellcheck: 'false' });
  const list = h('div', { class: 'pick-list', role: 'listbox', 'aria-label': title }, h('div', { class: 'pick-hint', text: 'Loading…' }));
  let options = [];
  const d = dialog({ title, content: h('div', { class: 'dialog-body' }, filter, list), buttons: [{ label: 'Cancel' }] });
  const draw = () => {
    const q = filter.value.trim().toLowerCase();
    const shown = options.filter((o) => !q || (o.label + ' ' + (o.note || '')).toLowerCase().includes(q));
    list.replaceChildren(...shown.map((o) => h('button', {
      class: 'pick' + (o.value === current ? ' on' : ''), role: 'option', 'aria-selected': String(o.value === current),
      onclick: () => {
        d.close();
        choose(o.value);
      },
    }, h('span', { class: 'pick-label', text: o.label }), o.note ? h('span', { class: 'pick-note', text: o.note }) : null)));
    if (!shown.length) list.append(h('div', { class: 'pick-hint', text: 'Nothing matches.' }));
  };
  filter.addEventListener('input', draw);
  filter.addEventListener('keydown', (e) => {
    if (e.key === 'ArrowDown') {
      e.preventDefault();
      const first = list.querySelector('button');
      if (first) first.focus();
    } else if (e.key === 'Enter') {
      const first = list.querySelector('button');
      if (first) first.click();
    }
  });
  list.addEventListener('keydown', (e) => {
    const all = [...list.querySelectorAll('button')];
    const i = all.indexOf(document.activeElement);
    if (e.key === 'ArrowDown' && i < all.length - 1) { e.preventDefault(); all[i + 1].focus(); }
    if (e.key === 'ArrowUp') { e.preventDefault(); (i > 0 ? all[i - 1] : filter).focus(); }
  });
  load().then((o) => {
    options = o;
    draw();
  }).catch((e) => list.replaceChildren(h('div', { class: 'pick-hint problem', text: e.message })));
  return d;
}

// ---- Notices ------------------------------------------------------------

let notices;

/** A notice at the top right of the working area: it goes by itself, an error after 8 s at least, never while the pointer rests on it. */
export function notice(text, { error = false, action = null, ms } = {}) {
  if (!notices) {
    notices = h('div', { class: 'notices', role: 'status', 'aria-live': 'polite' });
    document.body.append(notices);
  }
  const close = () => {
    n.classList.add('going');
    setTimeout(() => n.remove(), 180);
  };
  const n = h('div', { class: 'notice' + (error ? ' error' : '') },
    h('span', { class: 'notice-text', text }),
    action ? h('button', { class: 'notice-action', text: action.label, onclick: () => { close(); action.run(); } }) : null,
    h('button', { class: 'notice-x', 'aria-label': 'Close', text: '×', onclick: close }));
  notices.append(n);
  while (notices.children.length > 4) notices.firstElementChild.remove();
  let left = ms ?? (error ? 8000 : action ? 6500 : 4000);
  let timer = setTimeout(close, left);
  n.addEventListener('mouseenter', () => clearTimeout(timer));
  n.addEventListener('mouseleave', () => { timer = setTimeout(close, 2500); });
  return close;
}
