const CACHE = "kuraemail-v1";

self.addEventListener("install", (event) => {
  event.waitUntil(caches.open(CACHE).then((cache) => cache.addAll(["/icon.svg", "/css/app.css"])));
  self.skipWaiting();
});

self.addEventListener("activate", (event) => {
  event.waitUntil(
    caches.keys().then((keys) => Promise.all(keys.filter((key) => key !== CACHE).map((key) => caches.delete(key))))
  );
  self.clients.claim();
});

self.addEventListener("message", (event) => {
  if (event.data === "logout") {
    event.waitUntil(caches.delete(CACHE));
  }
});

self.addEventListener("fetch", (event) => {
  const request = event.request;
  if (request.method !== "GET") return;
  const url = new URL(request.url);
  if (url.origin !== self.location.origin) return;
  if (url.pathname === "/up" || url.pathname === "/service-worker") return;
  if (url.pathname.startsWith("/api/") || url.pathname === "/attachment") return;

  event.respondWith((async () => {
    try {
      const fresh = await fetch(request);
      if (fresh.ok && (url.pathname.startsWith("/css/") || url.pathname.startsWith("/js/") || url.pathname === "/icon.svg")) {
        const cache = await caches.open(CACHE);
        cache.put(request, fresh.clone());
      }
      return fresh;
    } catch {
      const cache = await caches.open(CACHE);
      const cached = await cache.match(request);
      if (cached) return cached;
      if (request.mode === "navigate") {
        return new Response(
          `<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>Tansu Email</title><p>Offline. <a href="/">Tansu Email</a></p>`,
          { status: 200, headers: { "Content-Type": "text/html; charset=utf-8" } }
        );
      }
      return Response.error();
    }
  })());
});
