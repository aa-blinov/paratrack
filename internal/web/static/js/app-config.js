// Template-provided text and small browser helpers shared by UI modules.
const data = document.body.dataset;
export const paratrackT = {
  sendError: data.i18nSendError,
  undo: data.i18nUndo,
  discard: data.i18nDiscard,
  needStartDuration: data.i18nNeedStartDuration,
  iosInstall: data.i18nIosInstall,
  offline: data.i18nOffline,
  offlinePending: data.i18nOfflinePending,
  syncing: data.i18nSyncing,
  queued: data.i18nQueued,
  queueFailed: data.i18nQueueFailed,
  queueNetworkFailed: data.i18nQueueNetworkFailed,
  queueRetry: data.i18nQueueRetry,
  queueDiscard: data.i18nQueueDiscard,
};

export const paratrackDecode = (value) => {
  if (!value) return value;
  try { return decodeURIComponent(value); } catch (_) { return value; }
};

export function paratrackCSRF() {
  const match = document.cookie.match(/(?:^|;\s*)paratrack_csrf=([^;]+)/);
  return match ? paratrackDecode(match[1]) : '';
}
