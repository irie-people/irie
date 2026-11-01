// Irie's service worker: it keeps the app's shell, so the app opens when the
// machine that served it is asleep and the others are awake. The shell comes
// from the network first, so an install lands at once, and from the cache
// when the network fails or takes too long. Nothing under /api/ is touched.
// Bump VERSION whenever the shell's file list changes.

const VERSION = 'irie-shell-1';
const SHELL = [
  './', 'index.html', 'app.css', 'app.js',
  'js/ui.js', 'js/look.js', 'js/net.js', 'js/md.js', 'js/conv.js',
  'manifest.webmanifest', 'icon-180.png', 'icon-192.png', 'icon-512.png',
];
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
  event.respondWith(shell(event, req, url));
});

async function shell(event, req, url) {
  const cache = await caches.open(VERSION);
  // Pages are the app's one page, whatever their query.
  const key = req.mode === 'navigate' ? new Request(new URL('./', self.location).href) : new Request(url.origin + url.pathname);
  const fresh = fetch(req, { cache: 'no-store' }).then((res) => {
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
