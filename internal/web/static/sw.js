// paratrack service worker — app shell cache, network-first for pages.
const CACHE = "paratrack-v5";
const SHELL = [
  "/static/css/paratrack.css",
  "/static/js/app.js",
  "/static/js/htmx.min.js",
  "/static/js/alpine.min.js",
  "/static/fonts/inter-latin.woff2",
  "/static/fonts/fraunces-latin.woff2",
  "/static/icons.svg",
  "/static/icon192.png",
  "/static/badge96.png",
  // App-shell pages so offline navigation works out of the box.
  "/",
  "/login",
];

self.addEventListener("install", (e) => {
  // Cache the shell one-by-one: addAll aborts on the first non-200
  // (e.g. "/" redirects to /login when logged out) and then offline
  // navigation has nothing to fall back to.
  e.waitUntil(
    caches.open(CACHE).then(async (c) => {
      await Promise.all(
        SHELL.map((u) =>
          fetch(u, { credentials: "same-origin" })
            .then((r) => {
              // Cache.put rejects redirected responses — skip them so a
              // logged-out "/" (303 → /login) doesn't break the install.
              if (r.ok && r.status === 200 && !r.redirected) {
                return c.put(u, r.clone());
              }
            })
            .catch(() => {})
        )
      );
      self.skipWaiting();
    })
  );
});

self.addEventListener("activate", (e) => {
  e.waitUntil(
    caches.keys()
      .then((keys) => Promise.all(keys.filter((k) => k !== CACHE).map((k) => caches.delete(k))))
      .then(() => self.clients.claim())
  );
});

self.addEventListener("fetch", (e) => {
  const url = new URL(e.request.url);
  // Never cache API or non-GET.
  if (e.request.method !== "GET" || url.pathname.startsWith("/api/")) return;
  // Static: cache-first (they are embedded + versioned by deploy).
  if (url.pathname.startsWith("/static/")) {
    e.respondWith(
      caches.match(e.request).then((hit) => hit || fetch(e.request).then((res) => {
        const copy = res.clone();
        caches.open(CACHE).then((c) => c.put(e.request, copy));
        return res;
      }))
    );
    return;
  }
  // Pages: network-first, fall back to cache when offline.
  e.respondWith(
    fetch(e.request)
      .then((res) => {
        if (res.ok && res.status === 200 && !res.redirected) {
          const copy = res.clone();
          caches.open(CACHE).then((c) => c.put(e.request, copy)).catch(() => {});
        }
        return res;
      })
      .catch(() => caches.match(e.request))
  );
});

// Web Push (Wave 9) — branded popup, monochrome badge, grouped by tag.
self.addEventListener("push", (e) => {
  let data = { title: "paratrack", body: "", url: "/", tag: "paratrack" };
  try { data = { ...data, ...e.data.json() }; } catch (_) {}
  e.waitUntil(self.registration.showNotification(data.title || "paratrack", {
    body: data.body || "",
    // Branded rounded icon (full colour) + monochrome badge (status bar).
    icon: "/static/icon192.png",
    badge: "/static/badge96.png",
    // Quiet, non-jumpy: one entry per kind, updated in place.
    tag: data.tag || "paratrack",
    renotify: true,
    requireInteraction: false,
    silent: false,
    vibrate: [40, 60, 40],
    data: { url: data.url || "/" },
    actions: [
      { action: "open", title: "Open" },
      { action: "dismiss", title: "Dismiss" },
    ],
  }));
});

self.addEventListener("notificationclick", (e) => {
  e.notification.close();
  if (e.action === "dismiss") return;
  const url = (e.notification.data && e.notification.data.url) || "/";
  e.waitUntil(clients.matchAll({ type: "window", includeUncontrolled: true }).then((list) => {
    for (const c of list) {
      if ("focus" in c) { c.navigate(url); return c.focus(); }
    }
    return clients.openWindow(url);
  }));
});
