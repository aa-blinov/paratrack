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
    // Reset links carry a one-time token in the query string. Keep page URLs
    // and navigation breadcrumbs useful without sending query values to Sentry.
    beforeSend(event) {
      if (event.request) {
        event.request.url = withoutQuery(event.request.url);
        event.request.query_string = '';
        if (event.request.headers) {
          for (const key of Object.keys(event.request.headers)) {
            if (key.toLowerCase() === 'referer') delete event.request.headers[key];
          }
        }
      }
      for (const breadcrumb of event.breadcrumbs || []) {
        const data = breadcrumb.data;
        if (!data) continue;
        for (const key of ['url', 'from', 'to']) {
          if (typeof data[key] === 'string') data[key] = withoutQuery(data[key]);
        }
      }
      return event;
    },
    // Errors only: no per-page session pings eating the quota.
    integrations: (all) => all.filter((i) => i.name !== 'BrowserSession'),
  });

  function withoutQuery(value) {
    if (typeof value !== 'string') return value;
    try {
      const url = new URL(value, window.location.origin);
      url.search = '';
      url.hash = '';
      return url.toString();
    } catch (_) {
      return value;
    }
  }
})();
