/* agentdeck service worker.

   The app shell is NETWORK-FIRST with a cache fallback: this is a self-hosted app
   that updates in place, and cache-first strands an installed phone on the
   previous build until CACHE_NAME moves. Everything else (font, icon) is
   cache-first because it is immutable.

   Non-GET requests return early: the cache API rejects them outright, and
   swallowing one here would break any POST the page makes. */
const CACHE = "agentdeck-v60";
const SHELL = ["/workspace-extension.js", "/agent-commands.js", "/sheet-focus.js", "/launch-profiles.js", "/launch-profiles.css", "/native-search.js", "/native-search.css", "/", "/command-palette.js", "/command-palette.css", "/session-groups.js", "/native-history.js", "/native-history.css", "/review.js", "/review.css", "/app.js", "/terminal-tabs.js", "/conversation.js", "/conversation.css", "/style.css", "/workspace.css", "/ui-menu.js", "/fonts.css", "/icon.svg", "/manifest.webmanifest"];

self.addEventListener("install", (e) => {
  e.waitUntil(caches.open(CACHE).then((c) => c.addAll(SHELL)).then(() => self.skipWaiting()));
});

self.addEventListener("activate", (e) => {
  e.waitUntil(caches.keys()
    .then((keys) => Promise.all(keys.filter((k) => k !== CACHE).map((k) => caches.delete(k))))
    .then(() => self.clients.claim()));
});

self.addEventListener("fetch", (e) => {
  if (e.request.method !== "GET") return;
  const url = new URL(e.request.url);
  if (url.origin !== location.origin) return;
  // never cache the API or the SSE streams — they are live state
  if (url.pathname.startsWith("/api/") || url.pathname.startsWith("/term/") || url.pathname.startsWith("/terminal/")) return;

  if (url.pathname.startsWith("/fonts/") || url.pathname === "/icon.svg") {
    e.respondWith(caches.match(e.request).then((hit) => hit || fetch(e.request)));
    return;
  }
  e.respondWith(
    fetch(e.request)
      .then((resp) => {
        const copy = resp.clone();
        caches.open(CACHE).then((c) => c.put(e.request, copy)).catch(() => {});
        return resp;
      })
      .catch(() => caches.match(e.request).then((hit) => hit || caches.match("/"))));
});

self.addEventListener("push", (e) => {
  let data = {};
  try { data = e.data ? e.data.json() : {}; } catch {}
  e.waitUntil(self.registration.showNotification(data.title || "agentdeck", {
    body: data.body || "", icon: "/icon.svg", badge: "/icon.svg",
    data: { url: data.url || "/" },
    actions: data.kind === "approval"
      ? [{ action: "open", title: "Review" }] : [],
  }));
});

self.addEventListener("notificationclick", (e) => {
  e.notification.close();
  const url = (e.notification.data && e.notification.data.url) || "/";
  e.waitUntil(clients.matchAll({ type: "window", includeUncontrolled: true })
    .then((list) => {
      for (const c of list) if ("focus" in c) return c.focus();
      return clients.openWindow(url);
    }));
});
