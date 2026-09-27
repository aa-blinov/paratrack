// Browser error reporting. Loaded only when the server has a Sentry DSN
// (base.html); events go through our /sentry-tunnel, the same way out as
// the Go server's, so no third-party origin is needed in CSP.
(function () {
  const m = document.querySelector('meta[name="sentry"]');
  if (!m || !window.Sentry) return;
  Sentry.init({
    dsn: m.content,
    tunnel: '/sentry-tunnel',
    release: m.dataset.release,
    environment: m.dataset.env,
    sendDefaultPii: false,
    // Errors only: no per-page session pings eating the quota.
    integrations: (all) => all.filter((i) => i.name !== 'BrowserSession'),
  });
})();
