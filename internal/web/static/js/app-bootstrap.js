// Runs synchronously in <head> so persisted theme and timezone are applied
// before styles render or page data is requested.
(function () {
  try {
    const mode = localStorage.getItem('paratrack-theme') || 'auto';
    const dark = mode === 'dark' || (mode === 'auto' && window.matchMedia('(prefers-color-scheme: dark)').matches);
    document.documentElement.dataset.theme = dark ? 'paratrack-dark' : 'paratrack-light';
    document.documentElement.dataset.themeMode = mode;
  } catch (_) {}

  try {
    const timezone = Intl.DateTimeFormat().resolvedOptions().timeZone;
    const stored = (document.cookie.match(/(?:^|; )paratrack_tz=([^;]*)/) || [])[1];
    if (timezone && decodeURIComponent(stored || '') !== timezone) {
      document.cookie = 'paratrack_tz=' + encodeURIComponent(timezone) + '; path=/; max-age=31536000; samesite=lax';
      if (stored) location.reload();
    }
  } catch (_) {}
})();
