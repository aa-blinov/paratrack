// Keep offline replay limited to the application mutations designed for
// at-least-once delivery. Credential and payment endpoints stay online-only.
const queueablePaths = {
  POST: [
    /^\/api\/start$/,
    /^\/api\/(?:active\/(?:pause-all|stop-all)|focus\/[^/]+)$/,
    /^\/api\/sessions\/(?:backfill|\d+\/(?:pause|resume|stop|reopen|tags))$/,
    /^\/api\/(?:activities\/\d+\/project|goals|tags|timesheet\/cell|schedule\/cell)$/,
  ],
  PATCH: [/^\/api\/sessions\/\d+$/],
  DELETE: [/^\/api\/sessions\/\d+(?:\/tags)?$/, /^\/api\/(?:goals|tags)$/],
};

export function isOfflineQueueableMutation(method, pathname) {
  const patterns = queueablePaths[String(method || '').toUpperCase()] || [];
  return patterns.some((pattern) => pattern.test(pathname));
}
