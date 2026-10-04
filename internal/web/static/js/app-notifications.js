// Push subscription controls are present only on the notifications page.
import { paratrackCSRF } from '/static/js/app-config.js';
import { apiFetch } from '/static/js/app-offline.js';

(function () {
  const statusEl = document.getElementById('push-status');
  if (!statusEl) return;

  const messages = {
    active: statusEl.dataset.msgActive || '',
    off: statusEl.dataset.msgOff || '',
    denied: statusEl.dataset.msgDenied || '',
    failed: statusEl.dataset.msgFailed || '',
  };
  const csrf = paratrackCSRF();

  function set(message) { statusEl.textContent = message; }
  function show(id, on) {
    const element = document.getElementById(id);
    if (element) element.classList.toggle('hidden', !on);
  }

  async function subscription() {
    if (!('serviceWorker' in navigator) || !('PushManager' in window)) return null;
    const registration = await navigator.serviceWorker.ready;
    return registration.pushManager.getSubscription();
  }

  async function refresh() {
    try {
      const sub = await subscription();
      show('push-enable', !sub);
      show('push-disable', !!sub);
      if (sub) set(messages.active);
      return sub;
    } catch (_) {
      show('push-enable', true);
      return null;
    }
  }

  function toUint8Array(base64) {
    const pad = '='.repeat((4 - (base64.length % 4)) % 4);
    const raw = atob((base64 + pad).replace(/-/g, '+').replace(/_/g, '/'));
    return Uint8Array.from(raw, (character) => character.charCodeAt(0));
  }

  async function enable() {
    try {
      const permission = await Notification.requestPermission();
      if (permission !== 'granted') {
        set(messages.denied);
        return;
      }
      const registration = await navigator.serviceWorker.ready;
      const keyResponse = await apiFetch('/api/push/key', { credentials: 'same-origin' });
      const { publicKey } = await keyResponse.json();
      const sub = await registration.pushManager.subscribe({
        userVisibleOnly: true,
        applicationServerKey: toUint8Array(publicKey),
      });
      const json = sub.toJSON();
      await apiFetch('/api/push/subscribe', {
        method: 'POST',
        credentials: 'same-origin',
        headers: { 'Content-Type': 'application/x-www-form-urlencoded', 'X-CSRF-Token': csrf },
        body: new URLSearchParams({
          csrf_token: csrf,
          endpoint: json.endpoint,
          p256dh: json.keys.p256dh,
          auth: json.keys.auth,
        }),
      });
      set(messages.active);
      await refresh();
    } catch (_) {
      set(messages.failed);
    }
  }

  async function disable() {
    try {
      const sub = await subscription();
      if (sub) {
        const json = sub.toJSON();
        await apiFetch('/api/push/unsubscribe', {
          method: 'POST',
          credentials: 'same-origin',
          headers: { 'Content-Type': 'application/x-www-form-urlencoded', 'X-CSRF-Token': csrf },
          body: new URLSearchParams({ csrf_token: csrf, endpoint: json.endpoint }),
        });
        await sub.unsubscribe();
      }
      await refresh();
      set(messages.off);
    } catch (_) {
      set(messages.failed);
    }
  }

  document.body.addEventListener('click', (event) => {
    const action = event.target.closest('[data-push-action]')?.dataset.pushAction;
    if (action === 'enable') enable();
    else if (action === 'disable') disable();
  });
  refresh();
})();
