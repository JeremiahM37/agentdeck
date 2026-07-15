/* agentdeck service worker — offline shell + push */
const CACHE = "agentdeck-v3";
const SHELL = ["/", "/style.css", "/app.js", "/icon.svg", "/manifest.webmanifest"];

self.addEventListener("install", (e) => {
  e.waitUntil(caches.open(CACHE).then((c) => c.addAll(SHELL)));
  self.skipWaiting();
});
self.addEventListener("activate", (e) => {
  e.waitUntil(caches.keys().then((keys) =>
    Promise.all(keys.filter((k) => k !== CACHE).map((k) => caches.delete(k)))));
  self.clients.claim();
});
self.addEventListener("fetch", (e) => {
  const url = new URL(e.request.url);
  if (url.pathname.startsWith("/api")) return;              // never cache data
  e.respondWith(
    fetch(e.request)
      .then((r) => {
        const copy = r.clone();
        caches.open(CACHE).then((c) => c.put(e.request, copy));
        return r;
      })
      .catch(() => caches.match(e.request).then((m) => m || caches.match("/")))
  );
});
self.addEventListener("push", (e) => {
  const data = e.data ? e.data.json() : {};
  const opts = {
    body: data.body || "", icon: "/icon.svg", badge: "/icon.svg",
    data: { url: data.url || "/", kind: data.kind, approval_id: data.approval_id },
  };
  if (data.kind === "approval")   // decide straight from the lock screen
    opts.actions = [{ action: "approve", title: "✅ Approve" },
                    { action: "deny", title: "⛔ Deny" }];
  e.waitUntil(self.registration.showNotification(data.title || "agentdeck", opts));
});
self.addEventListener("notificationclick", (e) => {
  e.notification.close();
  const d = e.notification.data || {};
  if (e.action && d.kind === "approval" && d.approval_id) {
    e.waitUntil(fetch(`/api/approvals/${d.approval_id}/decision`, {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ decision: e.action === "approve" ? "approved" : "denied",
                             note: e.action === "deny" ? "denied from notification" : "" }),
    }));
    return;
  }
  e.waitUntil(clients.openWindow(d.url || "/"));
});
