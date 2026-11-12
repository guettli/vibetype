// Service worker: caches the app shell so /t/{id} opens without network.
// Texts and unsynced progress live in localStorage (see app.js).
const CACHE = 'vibetype-v2';
const SHELL = ['/', '/static/app.js', '/static/engine.js', '/static/style.css'];

self.addEventListener('install', (e) => {
  e.waitUntil(caches.open(CACHE).then((c) => c.addAll(SHELL)).then(() => self.skipWaiting()));
});

self.addEventListener('activate', (e) => {
  e.waitUntil(caches.keys()
    .then((keys) => Promise.all(keys.filter((k) => k !== CACHE).map((k) => caches.delete(k))))
    .then(() => self.clients.claim()));
});

self.addEventListener('fetch', (e) => {
  const url = new URL(e.request.url);
  if (e.request.method !== 'GET' || url.origin !== location.origin || url.pathname.startsWith('/api/')) return;
  // network first, cache as fallback; every HTML page is the same SPA shell
  const key = e.request.mode === 'navigate' ? '/' : e.request;
  const cached = (orig) => caches.match(key).then((r) => r || orig || Promise.reject(new Error('offline and not cached')));
  e.respondWith(
    fetch(e.request)
      .then((res) => {
        if (res.ok) { const copy = res.clone(); caches.open(CACHE).then((c) => c.put(key, copy)); return res; }
        // backend down behind the reverse proxy (502/503/504): serve the cached shell
        if (res.status >= 500) return cached(res);
        return res;
      })
      .catch(() => cached(null)),
  );
});
