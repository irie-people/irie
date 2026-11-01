// Notifications by Web Push, which iOS gives Home Screen apps (16.4 and
// later). One subscription reaches this phone, and every paired machine
// pushes to it, so the phone makes the application server key (VAPID): one
// ECDSA P-256 pair, kept here, which each machine gets with the
// subscription and signs its pushes with.

import { pushTo, unpush } from './net.js';

const KEY = 'irie.vapid';
const PREFS = 'irie.push';

/** Whether this browser can take notifications at all. */
export const supported = () =>
  'serviceWorker' in navigator && 'PushManager' in window && 'Notification' in window && window.isSecureContext;

/** {on, done}: notifications on, and also when a task finishes. */
export function prefs() {
  try {
    return { on: false, done: false, ...JSON.parse(localStorage.getItem(PREFS) || '{}') };
  } catch {
    return { on: false, done: false };
  }
}

export function savePrefs(p) {
  localStorage.setItem(PREFS, JSON.stringify({ on: !!p.on, done: !!p.done }));
}

const b64u = (buf) => btoa(String.fromCharCode(...new Uint8Array(buf))).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');

function unb64u(s) {
  const t = s.replace(/-/g, '+').replace(/_/g, '/');
  return Uint8Array.from(atob(t + '='.repeat((4 - (t.length % 4)) % 4)), (c) => c.charCodeAt(0));
}

/** The phone's key: {public: 65-byte point, private: 32-byte scalar}, base64url, made once. */
export async function vapid() {
  try {
    const k = JSON.parse(localStorage.getItem(KEY) || 'null');
    if (k && k.public && k.private) return k;
  } catch {
    // Made again below.
  }
  const pair = await crypto.subtle.generateKey({ name: 'ECDSA', namedCurve: 'P-256' }, true, ['sign', 'verify']);
  const raw = await crypto.subtle.exportKey('raw', pair.publicKey);
  const jwk = await crypto.subtle.exportKey('jwk', pair.privateKey);
  const k = { public: b64u(raw), private: jwk.d };
  localStorage.setItem(KEY, JSON.stringify(k));
  return k;
}

/** The service worker's registration, or why there is none. */
async function registration() {
  const ready = navigator.serviceWorker.ready;
  const late = new Promise((_, no) => setTimeout(() => no(new Error('Irie’s service worker isn’t running. Close the app and open it again.')), 8000));
  return Promise.race([ready, late]);
}

/** The subscription with this phone's key, made when there is none. Needs permission. */
export async function subscription() {
  const reg = await registration();
  const k = await vapid();
  let sub = await reg.pushManager.getSubscription();
  const have = sub && sub.options && sub.options.applicationServerKey;
  if (sub && have && b64u(have) !== k.public) {
    // One made with another key can't carry these pushes.
    await sub.unsubscribe().catch(() => {});
    sub = null;
  }
  if (!sub) sub = await reg.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: unb64u(k.public) });
  return { sub, k };
}

/** Asks one machine to push to this phone. */
export function tell(m, sub, k, done) {
  return pushTo(m, { subscription: sub.toJSON(), vapid: { public: k.public, private: k.private }, done: !!done });
}

/** Asks one machine to stop. */
export const untell = (m) => unpush(m);

/** Ends this phone's subscription. */
export async function unsubscribe() {
  const reg = await navigator.serviceWorker.getRegistration();
  const sub = reg && (await reg.pushManager.getSubscription());
  if (sub) await sub.unsubscribe();
}
