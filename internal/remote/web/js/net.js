// Talking to machines: the paired ones this phone keeps, their gateway's
// HTTP API (`irie remote serve`), and the NDJSON streams of `subscribe`,
// which reconnect by themselves.

const KEY = 'irie.machines';

/** The machines this phone is paired with: [{id, name, url, token}]. */
export function loadMachines() {
  try {
    const list = JSON.parse(localStorage.getItem(KEY) || '[]');
    return Array.isArray(list) ? list.filter((m) => m && m.id && m.url && m.token) : [];
  } catch {
    return [];
  }
}

export function saveMachines(list) {
  localStorage.setItem(KEY, JSON.stringify(list.map(({ id, name, url, token }) => ({ id, name, url, token }))));
}

/** An error the gateway or the core gave, with its code and the words to show. */
export class ApiError extends Error {
  constructor(code, msg, status = 0) {
    super(msg || code || 'Something went wrong.');
    this.code = code || 'error';
    this.status = status;
  }
}

/** The error a fetch that never reached the machine gives. */
function unreachable(url, e) {
  if (e && e.name === 'AbortError') return new ApiError('timeout', `${host(url)} didn’t answer in time.`);
  return new ApiError('offline', `Can’t reach ${host(url)}. Is it awake and on this Wi-Fi?`);
}

export const host = (url) => {
  try {
    return new URL(url).host;
  } catch {
    return url;
  }
};

async function request(url, { method = 'GET', token, body, timeout = 15000 } = {}) {
  const ctl = new AbortController();
  const timer = setTimeout(() => ctl.abort(), timeout);
  const headers = {};
  if (token) headers.Authorization = 'Bearer ' + token;
  if (body !== undefined) headers['Content-Type'] = 'application/json';
  let r;
  try {
    r = await fetch(url, { method, headers, body: body === undefined ? undefined : JSON.stringify(body), signal: ctl.signal, cache: 'no-store' });
  } catch (e) {
    throw unreachable(url, e);
  } finally {
    clearTimeout(timer);
  }
  if (r.status === 204) return null;
  let j = null;
  try {
    j = await r.json();
  } catch {
    if (!r.ok) throw new ApiError('http_' + r.status, `${host(url)} answered ${r.status}.`, r.status);
  }
  if (!r.ok) {
    const e = (j && j.error) || {};
    throw new ApiError(e.code || 'http_' + r.status, e.msg || `${host(url)} answered ${r.status}.`, r.status);
  }
  return j;
}

/** GET /api/info: the machine's id, name, version and API, with no pairing. */
export const info = (url) => request(url + '/api/info', { timeout: 6000 });

/** POST /api/pair with the code the machine shows: {token, machine: {id, name}}. */
export function pair(url, code, device) {
  return request(url + '/api/pair', { method: 'POST', body: { code, device }, timeout: 10000 });
}

/** DELETE /api/device: the machine forgets this phone. */
export const forget = (m) => request(m.url + '/api/device', { method: 'DELETE', token: m.token, timeout: 6000 });

/**
 * POST /api/push: the machine notifies this phone through its push
 * subscription. The machine sends a first notification before it answers,
 * so this may take a while.
 */
export const pushTo = (m, body) => request(m.url + '/api/push', { method: 'POST', token: m.token, body, timeout: 30000 });

/** DELETE /api/push: the machine stops notifying this phone. */
export const unpush = (m) => request(m.url + '/api/push', { method: 'DELETE', token: m.token, timeout: 8000 });

/**
 * One op through /api/call: its result, or an ApiError with the core's
 * words. A 401 means the machine no longer knows this phone (code unpaired).
 */
export async function call(m, op, args = {}, { timeout = 15000 } = {}) {
  const j = await request(m.url + '/api/call', { method: 'POST', token: m.token, body: { op, args }, timeout });
  if (!j || j.ok !== true) {
    const e = (j && j.error) || {};
    throw new ApiError(e.code, e.msg);
  }
  return j.result ?? {};
}

/**
 * An address as typed, such as 10.0.0.7, studio.local, studio.local:7400 or
 * a full URL, as the gateway's origin: https and port 7387 unless it says.
 */
export function normalize(address) {
  const typed = String(address || '').trim();
  if (!typed) return '';
  const scheme = /^[a-z][a-z0-9+.-]*:\/\//i.test(typed);
  try {
    const u = new URL(scheme ? typed : 'https://' + typed);
    if (!/^https?:$/.test(u.protocol)) return '';
    const authority = (scheme ? typed.replace(/^[a-z][a-z0-9+.-]*:\/\//i, '') : typed).split(/[/?#]/)[0];
    if (!scheme && !/:\d+$/.test(authority)) u.port = '7387';
    return u.origin;
  } catch {
    return '';
  }
}

/**
 * A subscription: POST /api/subscribe, read as lines, one frame each. It
 * reconnects with backoff (0.5 s to 8 s) when the stream ends, as it does
 * when the core restarts, and takes 40 s without a byte as a dead stream.
 * kick() reconnects at once, for when the app comes back to the front.
 *
 * on.frame(frame), on.state('connecting'|'connected'|'offline'),
 * on.unpaired(msg), on.refused(error) → true to stop retrying.
 */
export class Stream {
  constructor(machine, args, on) {
    this.m = machine;
    this.args = args;
    this.on = on;
    this.delay = 500;
    this.gen = 0;
    this.stopped = true;
    this.connected = false;
    this.lastByte = 0;
  }

  start() {
    this.stopped = false;
    this.run();
  }

  stop() {
    this.stopped = true;
    this.gen++;
    clearTimeout(this.timer);
    if (this.ctl) this.ctl.abort();
  }

  /** Reconnects now, dropping a stream that may have died in the background. */
  kick() {
    if (this.stopped) return;
    clearTimeout(this.timer);
    this.delay = 500;
    this.run();
  }

  async run() {
    const gen = ++this.gen;
    if (this.ctl) this.ctl.abort();
    const ctl = new AbortController();
    this.ctl = ctl;
    if (!this.connected) this.on.state && this.on.state('connecting');
    let watchdog;
    const pet = () => {
      this.lastByte = Date.now();
      clearTimeout(watchdog);
      watchdog = setTimeout(() => ctl.abort(), 40000);
    };
    let retry = true;
    try {
      pet();
      const r = await fetch(this.m.url + '/api/subscribe', {
        method: 'POST',
        headers: { Authorization: 'Bearer ' + this.m.token, 'Content-Type': 'application/json' },
        body: JSON.stringify(this.args),
        signal: ctl.signal,
        cache: 'no-store',
      });
      if (r.status === 401) {
        const j = await r.json().catch(() => ({}));
        retry = false;
        this.connected = false;
        if (gen === this.gen) this.on.unpaired && this.on.unpaired((j.error && j.error.msg) || '');
        return;
      }
      if (!r.ok || !r.body) throw new Error('HTTP ' + r.status);
      const reader = r.body.getReader();
      const dec = new TextDecoder();
      let buf = '', first = true;
      for (;;) {
        const { value, done } = await reader.read();
        if (done || gen !== this.gen) break;
        pet();
        buf += dec.decode(value, { stream: true });
        let nl;
        while ((nl = buf.indexOf('\n')) >= 0) {
          const line = buf.slice(0, nl).trim();
          buf = buf.slice(nl + 1);
          if (!line) continue;
          let msg;
          try {
            msg = JSON.parse(line);
          } catch {
            continue;
          }
          if (first) {
            first = false;
            if (msg.ok === false) {
              const e = new ApiError(msg.error && msg.error.code, msg.error && msg.error.msg);
              if (this.on.refused && this.on.refused(e)) retry = false;
              throw e;
            }
            this.delay = 500;
            this.connected = true;
            this.since = Date.now();
            this.on.state && this.on.state('connected');
            if (msg.t) this.on.frame(msg);
            continue;
          }
          if (msg.t === 'ping') continue;
          this.on.frame(msg);
        }
      }
    } catch {
      // A refused, failed, aborted or dead stream: reconnect below.
    } finally {
      clearTimeout(watchdog);
    }
    if (this.stopped || gen !== this.gen) return;
    const was = this.connected;
    // One that keeps ending at once backs off, as one that never connects.
    const steady = was && Date.now() - this.since > 3000;
    this.connected = false;
    if (!retry) {
      this.on.state && this.on.state('offline');
      return;
    }
    // A stream that ended after it worked, as when the core restarts, is
    // only reconnecting; one that never connected is offline.
    this.on.state && this.on.state(was ? 'connecting' : 'offline');
    this.timer = setTimeout(() => this.run(), steady ? 300 : this.delay);
    if (!steady) this.delay = Math.min(this.delay * 2, 8000);
  }
}
