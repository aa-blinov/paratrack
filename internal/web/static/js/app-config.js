// Template-provided text and small browser helpers shared by UI modules.
const data = document.body.dataset;
export const paratrackT = {
  get sendError() { return data.i18nSendError; },
  get undo() { return data.i18nUndo; },
  get discard() { return data.i18nDiscard; },
  get needStartDuration() { return data.i18nNeedStartDuration; },
  get iosInstall() { return data.i18nIosInstall; },
  get offline() { return data.i18nOffline; },
  get offlinePending() { return data.i18nOfflinePending; },
  get syncing() { return data.i18nSyncing; },
  get queued() { return data.i18nQueued; },
  get queueFailed() { return data.i18nQueueFailed; },
  get queueNetworkFailed() { return data.i18nQueueNetworkFailed; },
  get queueRetry() { return data.i18nQueueRetry; },
  get queueDiscard() { return data.i18nQueueDiscard; },
};

export const paratrackDecode = (value) => {
  if (!value) return value;
  try { return decodeURIComponent(value); } catch (_) { return value; }
};

export function paratrackCSRF() {
  const match = document.cookie.match(/(?:^|;\s*)paratrack_csrf=([^;]+)/);
  return match ? paratrackDecode(match[1]) : '';
}
