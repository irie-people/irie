// Irie's service worker: it keeps the app's shell, so the app opens when the
// machine that served it is asleep and the others are awake. The shell comes
// from the network first, so an install lands at once, and from the cache
// when the network fails or takes too long. Nothing under /api/ is touched.
// It also shows the machines' notifications (Web Push) and opens the
// session one is about. Bump VERSION whenever this file or the shell's file
// list changes.

const VERSION = 'irie-shell-4';
const SHELL = [
  './', 'index.html', 'app.css', 'app.js',
  'js/ui.js', 'js/look.js', 'js/net.js', 'js/md.js', 'js/conv.js', 'js/push.js',
  'manifest.webmanifest', 'icon-180.png', 'icon-192.png', 'icon-512.png',
];
// Only the app's own page and files are kept, by their place in the scope:
// any other page of the origin, such as the setup page or the profile, goes
// to the network as it is and never stands in for the app.
const SCOPE = new URL(self.registration.scope);
const PAGES = new Set([SCOPE.pathname, new URL('index.html', SCOPE).pathname]);
const KEPT = new Set(SHELL.map((p) => new URL(p, SCOPE).pathname));
// How long the network may take before the cached shell is served instead;
// the network's answer still refreshes the cache when it comes.
const PATIENCE = 2500;

self.addEventListener('install', (event) => {
  event.waitUntil((async () => {
    const cache = await caches.open(VERSION);
    // One by one, so one missing file does not keep the rest out.
    await Promise.all(SHELL.map((url) => cache.add(new Request(url, { cache: 'reload' })).catch(() => {})));
    await self.skipWaiting();
  })());
});

self.addEventListener('activate', (event) => {
  event.waitUntil((async () => {
    for (const key of await caches.keys()) if (key !== VERSION) await caches.delete(key);
    await self.clients.claim();
  })());
});

self.addEventListener('fetch', (event) => {
  const req = event.request;
  if (req.method !== 'GET') return;
  const url = new URL(req.url);
  if (url.origin !== self.location.origin || url.pathname.startsWith('/api/')) return;
  if (req.mode === 'navigate' ? !PAGES.has(url.pathname) : !KEPT.has(url.pathname)) return;
  event.respondWith(shell(event, req, url));
});

async function shell(event, req, url) {
  const cache = await caches.open(VERSION);
  // The app's page is kept once, as the scope, whatever its query.
  const key = req.mode === 'navigate' ? new Request(SCOPE.href) : new Request(url.origin + url.pathname);
  // A navigation can't be copied with options, so it is asked for by its address.
  const fresh = (req.mode === 'navigate' ? fetch(url.href, { cache: 'no-store' }) : fetch(req, { cache: 'no-store' })).then((res) => {
    if (res.ok && res.type === 'basic') event.waitUntil(cache.put(key, res.clone()));
    return res;
  });
  event.waitUntil(fresh.catch(() => {}));
  const late = new Promise((resolve) => setTimeout(resolve, PATIENCE));
  try {
    const res = await Promise.race([fresh, late.then(() => null)]);
    if (res) return res;
  } catch {
    // Offline or refused: the cache below.
  }
  const cached = await cache.match(key);
  if (cached) return cached;
  return fresh;
}

// ---- Notifications ------------------------------------------------------------
// A machine's push carries {title, body, tag, url}: the tag is one per
// session, so a newer one replaces it, and url is the session's place in the
// app, such as #/m/<machine>/s/<session>.

self.addEventListener('push', (event) => {
  let n = {};
  try {
    n = event.data ? event.data.json() : {};
  } catch {
    try {
      n = { body: event.data.text() };
    } catch {
      n = {};
    }
  }
  const title = n.title || 'Irie';
  const options = {
    body: n.body || (n.title ? '' : 'Something changed'),
    icon: 'icon-192.png',
    data: { url: typeof n.url === 'string' ? n.url : '' },
  };
  if (n.tag) {
    options.tag = n.tag;
    options.renotify = true;
  }
  event.waitUntil(self.registration.showNotification(title, options));
});

self.addEventListener('notificationclick', (event) => {
  event.notification.close();
  const url = (event.notification.data && event.notification.data.url) || '';
  event.waitUntil((async () => {
    const open = await self.clients.matchAll({ type: 'window', includeUncontrolled: true });
    const app = open.find((c) => new URL(c.url).origin === self.location.origin);
    if (app) {
      // The app goes to the session itself, keeping what it has open.
      try {
        await app.focus();
      } catch {
        // Not every browser lets a worker focus a window.
      }
      app.postMessage({ open: url });
      return;
    }
    await self.clients.openWindow('./' + url);
  })());
});
