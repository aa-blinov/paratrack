// Only mutations designed for at-least-once delivery replay offline. Credential
// and payment endpoints stay online-only: a replay must never move money or
// change a password a second time.
const queueablePaths: Record<string, RegExp[]> = {
  POST: [
    /^\/api\/start$/,
    /^\/api\/(?:active\/(?:pause-all|stop-all)|focus\/[^/]+)$/,
    /^\/api\/sessions\/(?:backfill|\d+\/(?:pause|resume|stop|reopen|tags))$/,
    /^\/api\/(?:activities\/\d+\/project|goals|tags|timesheet\/cell|schedule\/cell)$/,
  ],
  PATCH: [/^\/api\/sessions\/\d+$/],
  DELETE: [/^\/api\/sessions\/\d+(?:\/tags)?$/, /^\/api\/(?:goals|tags)$/],
}

export function isOfflineQueueableMutation(method: string, pathname: string): boolean {
  const patterns = queueablePaths[method.toUpperCase()] ?? []
  return patterns.some(pattern => pattern.test(pathname))
}
