// Restore keyboard focus after the active-session list swaps its contents.
import { isKeyboardInputMode } from '/static/js/app-input-mode.js';

let requestedSession = null;

document.body.addEventListener('htmx:beforeRequest', (event) => {
  const path = event.detail.elt?.getAttribute?.('hx-post') || '';
  const match = path.match(/^\/api\/sessions\/(\d+)\/(pause|resume)$/);
  // A follow-up active-list refresh must not clear the row action's focus.
  if (match) requestedSession = isKeyboardInputMode() ? match[1] : null;
});

document.body.addEventListener('htmx:afterSettle', () => {
  if (!requestedSession) return;
  const row = document.querySelector(`#active-list [hx-post^="/api/sessions/${requestedSession}/"]`)?.closest('tr');
  if (!row) return;
  const button = row.querySelector('[hx-post$="/pause"], [hx-post$="/resume"]') || row.querySelector('button');
  requestedSession = null;
  button?.focus();
});
