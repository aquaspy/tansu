const CACHE = "kurapeople-v2";

self.addEventListener("install", (event) => {
  event.waitUntil(caches.open(CACHE).then((cache) => cache.addAll(["/icon.svg"])));
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

function cacheKey(request) {
  const url = new URL(request.url);
  for (const [key, value] of [...url.searchParams.entries()]) {
    if (value === "") url.searchParams.delete(key);
  }
  return url.pathname + url.search;
}

self.addEventListener("fetch", (event) => {
  const request = event.request;
  if (request.method !== "GET") return;

  const url = new URL(request.url);
  if (url.origin !== self.location.origin) return;
  if (url.pathname === "/cable" || url.pathname.startsWith("/cable/")) return;
  if (url.pathname === "/up" || url.pathname === "/service-worker") return;
  if (url.pathname === "/export") return;

  const key = cacheKey(request);

  event.respondWith((async () => {
    try {
      const fresh = await fetch(request);
      if (fresh.ok) {
        const cache = await caches.open(CACHE);
        cache.put(key, fresh.clone());
      }
      return fresh;
    } catch {
      const cache = await caches.open(CACHE);
      const cached = await cache.match(key);
      if (cached) return cached;
      if (request.mode === "navigate" || (request.headers.get("Accept") || "").includes("text/html")) {
        return new Response(
          `<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>TansuPeople</title><p>Offline. <a href="/">TansuPeople</a></p>`,
          { status: 200, headers: { "Content-Type": "text/html; charset=utf-8" } }
        );
      }
      return Response.error();
    }
  })());
});
