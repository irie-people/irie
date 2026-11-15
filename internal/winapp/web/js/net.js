// The page's one way out: messages through WebView2's window.chrome.webview
// to the app (internal/winapp), which carries them to Irie inside WSL and
// back. Calls to the core, the core's subscriptions, the slot's terminal,
// and what the window does itself (open a link, colour the title bar,
// notify, copy). Nothing here reaches the network.

const wv = window.chrome && window.chrome.webview;
const listeners = new Map();

/** Sends a message to the app. */
export function send(msg) {
  if (wv) wv.postMessage(JSON.stringify(msg));
}

/** Calls fn with each message of type t from the app; gives a function that stops it. */
export function on(t, fn) {
  if (!listeners.has(t)) listeners.set(t, new Set());
  listeners.get(t).add(fn);
  return () => listeners.get(t).delete(fn);
}

function dispatch(m) {
  const fns = listeners.get(m.t);
  if (fns) for (const fn of [...fns]) {
    try {
      fn(m);
    } catch (e) {
      console.error(e);
    }
  }
}

if (wv) {
  wv.addEventListener('message', (e) => {
    let m = e.data;
    if (typeof m === 'string') {
      try {
        m = JSON.parse(m);
      } catch {
        return;
      }
    }
    // The app sends what waited for its window's thread as one array.
    if (Array.isArray(m)) {
      for (const one of m) if (one && typeof one.t === 'string') dispatch(one);
    } else if (m && typeof m.t === 'string') dispatch(m);
  });
}

/** An error the core gave, with its code and the words to show as they are. */
export class ApiError extends Error {
  constructor(code, msg) {
    super(msg || code || 'Something went wrong.');
    this.code = code || 'error';
  }
}

let nextID = 1;
const pending = new Map();

on('reply', (m) => {
  const p = pending.get(m.id);
  if (!p) return;
  pending.delete(m.id);
  clearTimeout(p.timer);
  if (m.ok) p.resolve(m.result ?? {});
  else p.reject(new ApiError(m.error && m.error.code, m.error && m.error.msg));
});

/** One op of the core's: its result, or an ApiError in the core's words. */
export function call(op, args = {}, { timeout = 15000 } = {}) {
  const id = nextID++;
  return new Promise((resolve, reject) => {
    // The app answers by the timeout it was given; this is for an app
    // that never answers at all.
    const timer = setTimeout(() => {
      pending.delete(id);
      reject(new ApiError('timeout', 'Irie didn’t answer in time.'));
    }, timeout + 5000);
    pending.set(id, { resolve, reject, timer });
    send({ q: 'call', id, op, args, timeout });
  });
}

const streams = new Map();
on('sub', (m) => {
  const s = streams.get(m.id);
  if (!s) return;
  if (m.state === 'refused') {
    s.on.refused && s.on.refused(new ApiError(m.error && m.error.code, m.error && m.error.msg));
  } else {
    s.connected = m.state === 'connected';
    s.on.state && s.on.state(m.state);
  }
});
on('frame', (m) => {
  const s = streams.get(m.id);
  if (s) s.on.frame(m.f);
});

/**
 * A subscription to the core. The app subscribes again by itself when the
 * stream ends, as when the core restarts, so it only starts and stops.
 * on.frame(frame), on.state('connecting'|'connected'), on.refused(error).
 */
export class Stream {
  constructor(args, on) {
    this.args = args;
    this.on = on;
    this.id = 0;
    this.connected = false;
  }

  start() {
    if (this.id) return;
    this.id = nextID++;
    streams.set(this.id, this);
    this.on.state && this.on.state('connecting');
    send({ q: 'sub', id: this.id, args: this.args });
  }

  stop() {
    if (!this.id) return;
    streams.delete(this.id);
    send({ q: 'unsub', id: this.id });
    this.id = 0;
    this.connected = false;
  }
}

const terms = new Map();
on('term', (m) => {
  const t = terms.get(m.id);
  if (t) t(m);
});

/**
 * The slot's terminal: a tmux client of it in Linux, attached again by the
 * app whenever it detaches. onMsg hears {out} (base64) and {state, text}.
 */
export function openTerm(cols, rows, onMsg) {
  const id = nextID++;
  terms.set(id, onMsg);
  send({ q: 'term', id, op: 'open', cols, rows });
  return {
    keys: (data) => send({ q: 'term', id, op: 'in', data }),
    bytes: (b64) => send({ q: 'term', id, op: 'in', bin: b64 }),
    size: (c, r) => send({ q: 'term', id, op: 'size', cols: c, rows: r }),
    close: () => {
      terms.delete(id);
      send({ q: 'term', id, op: 'close' });
    },
  };
}

/** What the window does itself. */
export const host = {
  open: (url) => send({ q: 'open', url }),
  look: (l) => send({ q: 'look', ...l }),
  title: (title) => send({ q: 'title', title }),
  notify: (title, body) => send({ q: 'notify', title, body }),
  copy: (text) => send({ q: 'copy', body: text }),
  devtools: () => send({ q: 'devtools' }),
  fix: (step) => send({ q: 'fix', step }),
  retry: () => send({ q: 'retry' }),
};

/** Says the page is loaded: the app answers with how it stands. */
export const hello = () => send({ q: 'hello' });

/** Says the sessions show, once: Irie.exe --snapshot draws the window then. */
let rendered = false;
export function shown() {
  if (rendered) return;
  rendered = true;
  send({ q: 'rendered' });
}
