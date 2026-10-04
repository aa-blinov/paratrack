// Render offline and replay status without owning queue or network policy.
import { paratrackT } from '/static/js/app-config.js';

const fill = (template, value) => (template || '').replace('{n}', value);

export function createOfflineBanner(onAction) {
  let element;
  let latestState = { online: true, queueLength: 0, blockedStatus: null };

  function ensureElement() {
    if (element?.isConnected) return element;
    element = document.getElementById('offline-banner');
    if (!element) {
      element = document.createElement('div');
      element.id = 'offline-banner';
      element.className = 'offline-banner';
      element.hidden = true;
      element.setAttribute('role', 'status');
      element.setAttribute('aria-live', 'polite');
      document.body.appendChild(element);
    }
    element.addEventListener('click', (event) => {
      const action = event.target.closest('[data-offline-queue-action]')?.dataset.offlineQueueAction;
      if (action === 'retry' || action === 'discard') onAction(action);
    });
    return element;
  }

  function render(state) {
    latestState = state;
    const el = ensureElement();
    const { online, queueLength, blockedStatus } = state;
    if (blockedStatus !== null && queueLength > 0) {
      const message = document.createElement('span');
      message.textContent = blockedStatus === 0
        ? (paratrackT.queueNetworkFailed || paratrackT.sendError)
        : fill(paratrackT.queueFailed, blockedStatus);
      const actions = document.createElement('div');
      actions.className = 'offline-banner-actions';
      for (const [action, label] of [['retry', paratrackT.queueRetry], ['discard', paratrackT.queueDiscard]]) {
        const button = document.createElement('button');
        button.type = 'button';
        button.className = 'offline-banner-action';
        button.dataset.offlineQueueAction = action;
        button.textContent = label;
        actions.append(button);
      }
      el.replaceChildren(message, actions);
      el.hidden = false;
      return;
    }
    if (!online) {
      el.hidden = false;
      el.textContent = queueLength ? fill(paratrackT.offlinePending, queueLength) : paratrackT.offline;
    } else if (queueLength) {
      el.hidden = false;
      el.textContent = fill(paratrackT.syncing, queueLength);
      setTimeout(() => {
        if (navigator.onLine && latestState.online && latestState.queueLength === 0) el.hidden = true;
      }, 1200);
    } else {
      el.hidden = true;
    }
  }

  return { render };
}
