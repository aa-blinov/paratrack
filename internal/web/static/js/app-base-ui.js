// Shared HTMX response feedback and navigation behavior.
import { paratrackDecode, paratrackT } from '/static/js/app-config.js';
import { paratrackToast } from '/static/js/app-toast.js';

document.body.addEventListener('htmx:afterRequest', (event) => {
  const xhr = event.detail.xhr;
  const message = paratrackDecode(xhr.getResponseHeader('X-Toast'));
  if (message) {
    const actions = [];
    const undo = xhr.getResponseHeader('X-Toast-Undo');
    const discard = xhr.getResponseHeader('X-Toast-Discard');
    if (undo) actions.push({ label: paratrackT.undo, method: 'POST', url: undo });
    if (discard) actions.push({ label: paratrackT.discard, method: 'DELETE', url: discard, quiet: true });
    paratrackToast(message, xhr.getResponseHeader('X-Toast-Kind') || 'success', 0, actions);
  }

  const element = event.detail.elt;
  if (element) {
    element.classList.remove('btn-disabled');
    element.removeAttribute('aria-busy');
  }
});

document.body.addEventListener('htmx:beforeRequest', (event) => {
  const element = event.detail.elt;
  if (element && (element.tagName === 'BUTTON' || element.classList.contains('btn'))) {
    element.classList.add('btn-disabled');
    element.setAttribute('aria-busy', 'true');
  }
});
