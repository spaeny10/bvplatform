// Ironsight service worker.
//
// Two jobs:
//   1. Make the app installable as a PWA. Browsers gate the
//      "install this app" prompt on (a) a valid manifest and (b) a
//      registered service worker, even if the worker doesn't do any
//      caching. So we register one regardless of features.
//   2. Provide a graceful offline shell. When the customer's phone
//      drops to LTE-zero on a job site, we serve the last-cached
//      portal index instead of Chrome's broken-dino page.
//
// Strategy: cache-first for hashed Next.js static assets (they're
// content-addressed, never change for a given URL); network-first
// for navigation requests with a cached-shell fallback. We
// deliberately do NOT cache /api/*, /auth/*, /ws — live data must
// always go to the network because stale alarms are worse than no
// alarms. The fetch handler skips those paths entirely.

const VERSION = 'ironsight-sw-v1';
const STATIC_CACHE = `static-${VERSION}`;
const RUNTIME_CACHE = `runtime-${VERSION}`;
const OFFLINE_URL = '/offline';

// Install: pre-cache the offline fallback page so it's available the
// first time we lose network. Browsers run this once, then activate.
self.addEventListener('install', (event) => {
  event.waitUntil(
    caches.open(STATIC_CACHE).then((cache) => cache.addAll([OFFLINE_URL])),
  );
  self.skipWaiting();
});

// Activate: drop caches from old SW versions so a deploy doesn't
// leave the user pinned to last week's chunks forever.
self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches.keys().then((keys) =>
      Promise.all(
        keys
          .filter((k) => !k.endsWith(VERSION))
          .map((k) => caches.delete(k)),
      ),
    ),
  );
  self.clients.claim();
});

self.addEventListener('fetch', (event) => {
  const req = event.request;
  if (req.method !== 'GET') return;

  const url = new URL(req.url);

  // Same-origin only — we don't proxy other domains.
  if (url.origin !== self.location.origin) return;

  // Live-data paths bypass the cache entirely. Stale evidence is
  // worse than no evidence; stale alarms could mislead a customer
  // into thinking they got SOC attention they didn't.
  if (url.pathname.startsWith('/api/') ||
      url.pathname.startsWith('/auth/') ||
      url.pathname.startsWith('/ws') ||
      url.pathname.startsWith('/share/') ||
      url.pathname.startsWith('/hls/') ||
      url.pathname.startsWith('/recordings/') ||
      url.pathname.startsWith('/snapshots/') ||
      url.pathname.startsWith('/exports/')) {
    return;
  }

  // Cache-first for hashed static assets — they have content hashes
  // baked into the URL, so a stale one is impossible.
  if (url.pathname.startsWith('/_next/static/') ||
      url.pathname.startsWith('/icons/') ||
      url.pathname === '/manifest.webmanifest') {
    event.respondWith(
      caches.match(req).then((hit) =>
        hit || fetch(req).then((res) => {
          if (res.ok) {
            const copy = res.clone();
            caches.open(RUNTIME_CACHE).then((c) => c.put(req, copy));
          }
          return res;
        }),
      ),
    );
    return;
  }

  // Navigation requests: network-first, fall back to cached shell or
  // the offline page. This keeps the lock-screen / home-screen
  // launch usable even with no signal — the customer sees their
  // last-known state until reconnect.
  if (req.mode === 'navigate') {
    event.respondWith(
      fetch(req)
        .then((res) => {
          // Cache successful navigations so the shell is always
          // recoverable. We cache by URL so /portal, /portal/sites/X,
          // and /status each have their own most-recent snapshot.
          if (res.ok) {
            const copy = res.clone();
            caches.open(RUNTIME_CACHE).then((c) => c.put(req, copy));
          }
          return res;
        })
        .catch(() =>
          caches.match(req).then((hit) => hit || caches.match(OFFLINE_URL)),
        ),
    );
  }
});
