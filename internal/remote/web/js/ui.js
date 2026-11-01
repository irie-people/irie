// The small pieces every screen uses: making elements, bottom sheets,
// action sheets that confirm in place, and toasts.

/** An element: h('div', {class, text, html, onclick, …}, ...children). */
export function h(tag, props, ...kids) {
  const el = document.createElement(tag);
  if (props) {
    for (const [k, v] of Object.entries(props)) {
      if (v == null || v === false) continue;
      if (k === 'class') el.className = v;
      else if (k === 'text') el.textContent = v;
      else if (k === 'html') el.innerHTML = v;
      else if (k === 'value') el.value = v;
      else if (k.startsWith('on') && typeof v === 'function') el.addEventListener(k.slice(2), v);
      else if (typeof v === 'boolean' && k in el) el[k] = v;
      else el.setAttribute(k, v === true ? '' : String(v));
    }
  }
  for (const kid of kids.flat(Infinity)) {
    if (kid != null && kid !== false) el.append(kid instanceof Node ? kid : String(kid));
  }
  return el;
}

export const $ = (sel, root = document) => root.querySelector(sel);

export const ICON = {
  back: '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M15 4.5 7.5 12l7.5 7.5" fill="none" stroke="currentColor" stroke-width="2.6" stroke-linecap="round" stroke-linejoin="round"/></svg>',
  more: '<svg viewBox="0 0 24 24" aria-hidden="true"><circle cx="5.5" cy="12" r="2" fill="currentColor"/><circle cx="12" cy="12" r="2" fill="currentColor"/><circle cx="18.5" cy="12" r="2" fill="currentColor"/></svg>',
  close: '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M7 7l10 10M17 7 7 17" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round"/></svg>',
  up: '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 19V5.5M6 11.5l6-6 6 6" fill="none" stroke="currentColor" stroke-width="2.6" stroke-linecap="round" stroke-linejoin="round"/></svg>',
  down: '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 5v13.5M6 12.5l6 6 6-6" fill="none" stroke="currentColor" stroke-width="2.6" stroke-linecap="round" stroke-linejoin="round"/></svg>',
  chevron: '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M9 5l7 7-7 7" fill="none" stroke="currentColor" stroke-width="2.6" stroke-linecap="round" stroke-linejoin="round"/></svg>',
  caret: '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M7 10l5 5 5-5" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round"/></svg>',
  share: '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 3.5v11M8 7.5l4-4 4 4" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round"/><path d="M8.5 10.5H7a1.5 1.5 0 0 0-1.5 1.5v7A1.5 1.5 0 0 0 7 20.5h10a1.5 1.5 0 0 0 1.5-1.5v-7a1.5 1.5 0 0 0-1.5-1.5h-1.5" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round"/></svg>',
  add: '<svg viewBox="0 0 24 24" aria-hidden="true"><rect x="4" y="4" width="16" height="16" rx="4" fill="none" stroke="currentColor" stroke-width="1.9"/><path d="M12 8.5v7M8.5 12h7" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round"/></svg>',
  clock: '<svg viewBox="0 0 16 16" aria-hidden="true"><circle cx="8" cy="8" r="6.2" fill="none" stroke="currentColor" stroke-width="1.5"/><path d="M8 4.6V8l2.3 1.5" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/></svg>',
  ok: '<svg viewBox="0 0 16 16" aria-hidden="true"><circle cx="8" cy="8" r="7" fill="currentColor"/><path d="M4.9 8.2 7 10.2l4.1-4.5" fill="none" stroke="var(--on-mark)" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"/></svg>',
  err: '<svg viewBox="0 0 16 16" aria-hidden="true"><circle cx="8" cy="8" r="7" fill="currentColor"/><path d="M5.6 5.6l4.8 4.8M10.4 5.6l-4.8 4.8" fill="none" stroke="var(--on-mark)" stroke-width="1.8" stroke-linecap="round"/></svg>',
  stop: '<svg viewBox="0 0 16 16" aria-hidden="true"><circle cx="8" cy="8" r="6.3" fill="none" stroke="currentColor" stroke-width="1.4"/><path d="M5.2 8h5.6" stroke="currentColor" stroke-width="1.6" stroke-linecap="round"/></svg>',
  ring: '<svg class="spin" viewBox="0 0 16 16" aria-hidden="true"><circle cx="8" cy="8" r="5.4" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-dasharray="24 34"/></svg>',
  machine: '<svg viewBox="0 0 24 24" aria-hidden="true"><rect x="3" y="4.5" width="18" height="12" rx="2.2" fill="none" stroke="currentColor" stroke-width="1.8"/><path d="M8.5 20h7M12 16.5V20" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"/></svg>',
  terminal: '<svg viewBox="0 0 24 24" aria-hidden="true"><rect x="3" y="4.5" width="18" height="15" rx="2.5" fill="none" stroke="currentColor" stroke-width="1.8"/><path d="M7 9.5l3 2.5-3 2.5M12 15h5" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"/></svg>',
};

// ---- Sheets ---------------------------------------------------------------

let current = null;
let host = () => document.getElementById('app');

/** The open sheet, if any. */
export const openSheet = () => current;

/** Closes the open sheet. */
export function closeSheet() {
  if (current) current.close();
}

/**
 * A sheet that slides up from the bottom. content is an element; title, if
 * given, heads it with a close button. Returns {close, panel, body, swap}.
 */
export function sheet({ title, content, label, className = '', onClose }) {
  if (current) current.close(true);
  const body = h('div', { class: 'sheet-body' }, content);
  const head = title
    ? h('div', { class: 'sheet-head' },
        h('h2', { class: 'sheet-title', text: title }),
        h('button', { class: 'icon-btn sheet-x', 'aria-label': 'Close', html: ICON.close, onclick: () => s.close() }))
    : null;
  const panel = h('div', { class: 'sheet ' + className, role: 'dialog', 'aria-modal': 'true', 'aria-label': label || title || 'Options', tabindex: '-1' },
    h('div', { class: 'grabber', 'aria-hidden': 'true' }), head, body);
  const backdrop = h('div', { class: 'backdrop' });
  const layer = h('div', { class: 'layer' }, backdrop, panel);
  const before = document.activeElement;
  let closed = false;
  const s = {
    panel, body, layer,
    close(instant) {
      if (closed) return;
      closed = true;
      if (current === s) current = null;
      document.removeEventListener('keydown', onKey);
      layer.classList.remove('open');
      const done = () => layer.remove();
      if (instant || matchMedia('(prefers-reduced-motion: reduce)').matches) done();
      else setTimeout(done, 260);
      if (before && before.focus && document.contains(before) && !instant) before.focus({ preventScroll: true });
      onClose && onClose();
    },
    /** Puts other content in the sheet, such as a confirmation. */
    swap(node) {
      body.replaceChildren(node);
    },
  };
  const onKey = (e) => {
    if (e.key === 'Escape') s.close();
  };
  document.addEventListener('keydown', onKey);
  backdrop.addEventListener('click', () => s.close());
  dragToDismiss(panel, () => s.close());
  host().append(layer);
  current = s;
  // Two frames, so the panel starts below the edge and slides up.
  requestAnimationFrame(() => requestAnimationFrame(() => layer.classList.add('open')));
  if (!panel.querySelector('input, textarea')) panel.focus({ preventScroll: true });
  return s;
}

/** A downward drag on the grabber or the head takes a sheet away. */
function dragToDismiss(panel, close) {
  let y0 = null, dy = 0, id = null;
  panel.addEventListener('pointerdown', (e) => {
    if (!e.target.closest('.grabber, .sheet-head, .action-head') || e.target.closest('button')) return;
    y0 = e.clientY;
    dy = 0;
    id = e.pointerId;
    panel.setPointerCapture(id);
    panel.style.transition = 'none';
  });
  panel.addEventListener('pointermove', (e) => {
    if (y0 == null || e.pointerId !== id) return;
    dy = Math.max(0, e.clientY - y0);
    panel.style.transform = `translateY(${dy}px)`;
  });
  const end = (e) => {
    if (y0 == null || e.pointerId !== id) return;
    y0 = null;
    panel.style.transition = '';
    panel.style.transform = '';
    if (dy > 90) close();
  };
  panel.addEventListener('pointerup', end);
  panel.addEventListener('pointercancel', end);
}

/**
 * An action sheet: a list of buttons and Cancel. An item with confirm asks
 * again inside the sheet before it runs: {label, run, destructive, confirm:
 * {title, message, label}, checked, note}.
 */
export function actions({ title, message, items }) {
  const shown = items.filter(Boolean).filter((i) => !i.hidden);
  const group = h('div', { class: 'action-group' });
  const s = sheet({
    className: 'action-sheet',
    label: title || 'Actions',
    content: h('div', { class: 'actions' },
      h('div', { class: 'action-group-wrap' },
        title || message ? h('div', { class: 'action-head' },
          title && h('div', { class: 'action-title', text: title }),
          message && h('div', { class: 'action-msg', text: message })) : null,
        group),
      h('button', { class: 'action cancel', text: 'Cancel', onclick: () => s.close() })),
  });
  const button = (it) => h('button', {
    class: 'action' + (it.destructive ? ' destructive' : '') + (it.checked ? ' checked' : ''),
    disabled: !!it.disabled,
    onclick: () => {
      if (it.confirm) return confirmIn(s, it);
      s.close();
      it.run && it.run();
    },
  }, h('span', { class: 'action-label', text: it.label }), it.note ? h('span', { class: 'action-note', text: it.note }) : null);
  // An item with row puts its own items side by side, such as Rate good and Rate bad.
  for (const it of shown) group.append(it.row ? h('div', { class: 'action-row' }, it.row.filter(Boolean).map(button)) : button(it));
  return s;
}

/** Asks inside the sheet before a destructive action runs. */
function confirmIn(s, it) {
  const c = it.confirm;
  s.swap(h('div', { class: 'actions' },
    h('div', { class: 'action-group-wrap' },
      h('div', { class: 'action-head' },
        h('div', { class: 'action-title', text: c.title || it.label }),
        c.message && h('div', { class: 'action-msg', text: c.message })),
      h('div', { class: 'action-group' },
        h('button', {
          class: 'action destructive strong',
          text: c.label || it.label,
          onclick: () => {
            s.close();
            it.run && it.run();
          },
        }))),
    h('button', { class: 'action cancel', text: 'Cancel', onclick: () => s.close() })));
  const b = s.panel.querySelector('.action.destructive');
  b && b.focus({ preventScroll: true });
}

/** A sheet that asks for one line of text, such as a new name. */
export function ask({ title, value = '', placeholder = '', okLabel = 'Save', hint, run }) {
  const input = h('input', { class: 'text-in', type: 'text', value, placeholder, enterkeyhint: 'done', autocapitalize: 'sentences', autocomplete: 'off' });
  const problem = h('div', { class: 'problem', role: 'alert' });
  const ok = h('button', { class: 'btn primary wide', text: okLabel, type: 'submit' });
  const form = h('form', { class: 'ask' }, input, hint ? h('div', { class: 'hint', text: hint }) : null, problem, ok);
  const s = sheet({ title, content: form });
  form.addEventListener('submit', async (e) => {
    e.preventDefault();
    const v = input.value.trim();
    if (!v) return;
    ok.disabled = true;
    problem.textContent = '';
    try {
      await run(v);
      s.close();
    } catch (err) {
      problem.textContent = err.message || String(err);
      ok.disabled = false;
    }
  });
  setTimeout(() => {
    input.focus();
    input.select();
  }, 60);
  return s;
}

// ---- Toasts ---------------------------------------------------------------

/**
 * A toast above the bottom bar: the core's words as it wrote them, with an
 * action such as Undo. Errors stay longer.
 */
export function toast(text, { error = false, action = null, ms } = {}) {
  const box = document.getElementById('toasts');
  if (!box) return null;
  const el = h('div', { class: 'toast' + (error ? ' error' : ''), role: error ? 'alert' : 'status' },
    h('div', { class: 'toast-text', text }));
  let timer;
  const dismiss = () => {
    clearTimeout(timer);
    el.classList.add('out');
    setTimeout(() => el.remove(), 220);
  };
  if (action) {
    el.append(h('button', {
      class: 'toast-action',
      text: action.label,
      onclick: (e) => {
        e.stopPropagation();
        dismiss();
        action.run();
      },
    }));
  }
  el.addEventListener('click', dismiss);
  box.append(el);
  while (box.children.length > 3) box.firstElementChild.remove();
  timer = setTimeout(dismiss, ms || (action ? 6500 : error ? 7000 : 3800));
  return { dismiss };
}
