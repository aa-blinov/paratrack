// PWA installation, offline status, and running-timer badge.
import { paratrackT } from '/static/js/app-config.js';
import { paratrackToast } from '/static/js/app-toast.js';

// ---- PWA: install, offline, running-timer badge ----
(function () {
  const T = paratrackT;
  const standalone = matchMedia('(display-mode: standalone)').matches || navigator.standalone === true;
  const ios = /iphone|ipad|ipod/i.test(navigator.userAgent);
  let deferred = null;
  const showInstall = (on) => document.querySelectorAll('[data-install]').forEach((el) => { el.hidden = !on; });

  if ('serviceWorker' in navigator) {
    window.addEventListener('load', () => navigator.serviceWorker.register('/sw.js', { scope: '/' }).catch(() => {}));
  }

  // Chrome / Edge / Android: keep the native prompt for our own button.
  window.addEventListener('beforeinstallprompt', (e) => { e.preventDefault(); deferred = e; showInstall(true); });
  window.addEventListener('appinstalled', () => { deferred = null; showInstall(false); });
  // iOS Safari has no prompt: the button explains Share -> Add to Home Screen.
  if (ios && !standalone) showInstall(true);
  document.addEventListener('click', async (e) => {
    if (!e.target.closest('[data-install]')) return;
    e.preventDefault();
    if (deferred) {
      deferred.prompt();
      await deferred.userChoice;
      deferred = null;
      showInstall(false);
    } else if (ios) {
      paratrackToast(T.iosInstall, 'info', 8000);
    }
  });

  // Offline actions are queued by the Wave 9 block above. Online but the
  // server did not answer: say so instead of failing silently.
  document.body.addEventListener('htmx:sendError', () => {
    if (navigator.onLine) paratrackToast(T.sendError, 'error', 5000);
  });

  // App shortcut "New timer" lands on /?focus=activity.
  if (new URLSearchParams(location.search).get('focus') === 'activity') {
    document.getElementById('activity')?.focus();
  }

  // Running timers: count on the app icon (installed PWA) and in the tab title.
  const baseTitle = document.title;
  const sync = () => {
    const list = document.getElementById('active-list');
    if (!list) return;
    const n = list.querySelectorAll('.status-pill.is-active').length;
    document.title = n ? `\u25cf ${n} \u00b7 ${baseTitle}` : baseTitle;
    if ('setAppBadge' in navigator) (n ? navigator.setAppBadge(n) : navigator.clearAppBadge()).catch(() => {});
  };
  document.body.addEventListener('htmx:afterSettle', sync);
  sync();
})();
