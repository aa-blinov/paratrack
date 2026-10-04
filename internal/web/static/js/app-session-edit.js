// Inline historical-session duration editing.
import { paratrackCSRF, paratrackDecode, paratrackT } from '/static/js/app-config.js';
import { paratrackToast } from '/static/js/app-toast.js';
import { apiFetch, OfflineQueuedError } from '/static/js/app-offline.js';

document.addEventListener('change', (event) => {
  const input = event.target.closest('[data-resize-session]');
  if (input) resizeSession(input, input.dataset.resizeSession);
});

// resizeSession — parses a human duration ("1h 30m") from the
// inline edit field, recomputes end_at from start_at, and PATCHes
// the same endpoint the end_at input would.
export async function resizeSession(input, sessionID) {
  const row = document.getElementById('row-' + sessionID);
  if (!row) return;
  const startInput = row.querySelector('[name="start_at"]');
  const endInput = row.querySelector('[name="end_at"]');
  const noteInput = row.querySelector('[name="note"]');
  if (!startInput || !endInput || !noteInput) return;

  const startVal = startInput.value;
  const endVal = endInput.value;
  const noteVal = noteInput.value;
  const durVal = input.value.trim();

  if (!startVal || !durVal) {
    paratrackToast(paratrackT.needStartDuration, 'error');
    return;
  }

  // Build a form and submit via fetch + htmx-style swap.
  const fd = new FormData();
  fd.set('start_at', startVal);
  fd.set('end_at', endVal); // server overrides when duration is present
  fd.set('duration', durVal);
  fd.set('note', noteVal);
  try {
    const resp = await apiFetch('/api/sessions/' + sessionID, {
      method: 'PATCH',
      body: new URLSearchParams([...fd.entries()]),
      headers: {
        'Content-Type': 'application/x-www-form-urlencoded',
        'X-CSRF-Token': paratrackCSRF(),
      },
    });
    if (!resp.ok) {
      const message = (await resp.text()).trim();
      paratrackToast(message || paratrackT.sendError, 'error');
      return;
    }
    const html = await resp.text();
    // Toast from response header.
    const toast = paratrackDecode(resp.headers.get('X-Toast'));
    if (toast) paratrackToast(toast, resp.headers.get('X-Toast-Kind') || 'success');
    // HTMX-style outer swap. htmx.process() is required: content injected
    // outside htmx.load() is inert until processed, which used to kill the
    // row's tag / pause / delete bindings after an inline duration edit.
    const tmp = document.createElement('tbody');
    tmp.innerHTML = html.trim();
    const newRow = tmp.firstElementChild;
    if (newRow) {
      row.outerHTML = newRow.outerHTML;
      const live = document.getElementById('row-' + sessionID);
      if (live && window.htmx) window.htmx.process(live);
    }
  } catch (e) {
    if (e instanceof OfflineQueuedError) return;
    paratrackToast(paratrackT.sendError, 'error');
  }
}
