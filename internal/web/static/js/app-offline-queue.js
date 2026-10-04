// Serialization and validation for the durable offline mutation queue.
export function readOfflineQueue(storage, key) {
  try {
    const value = JSON.parse(storage.getItem(key) || '[]');
    return Array.isArray(value) ? value : [];
  } catch (_) {
    return [];
  }
}

export function writeOfflineQueue(storage, key, items) {
  try {
    storage.setItem(key, JSON.stringify(items));
    return true;
  } catch (_) {
    return false;
  }
}

export function isValidOfflineQueueItem(item) {
  if (!item || typeof item !== 'object' || Array.isArray(item) ||
      typeof item.verb !== 'string' || typeof item.url !== 'string') return false;
  if (item.ts !== undefined && (!Number.isFinite(item.ts) || typeof item.ts !== 'number')) return false;
  if (item.encoding === 'json') {
    return item.body !== null && typeof item.body === 'object' && !Array.isArray(item.body);
  }
  return item.encoding === 'form' && Array.isArray(item.body) &&
    item.body.every((entry) => Array.isArray(entry) && entry.length === 2 &&
      entry.every((value) => typeof value === 'string'));
}

export function sameOriginPath(rawURL, pageURL) {
  try {
    const page = new URL(pageURL);
    const url = new URL(rawURL, page);
    if (url.origin !== page.origin) return null;
    return url.pathname + url.search;
  } catch (_) {
    return null;
  }
}

function formEntries(body) {
  if (body == null) return [];
  try {
    if (typeof URLSearchParams !== 'undefined' && body instanceof URLSearchParams) {
      return [...body.entries()];
    }
    if (typeof FormData !== 'undefined' && body instanceof FormData) {
      const entries = [...body.entries()];
      return entries.every(([, value]) => typeof value === 'string') ? entries : null;
    }
    if (typeof body === 'string') return [...new URLSearchParams(body).entries()];
    if (Object.getPrototypeOf(body) === Object.prototype) {
      return Object.entries(body).flatMap(([key, value]) =>
        (Array.isArray(value) ? value : [value]).map(item => [key, String(item)]));
    }
  } catch (_) {}
  return null;
}

export function encodeOfflineRequestPayload(body, contentType) {
  const isJSON = /(?:^|\s|;)application\/(?:[\w.+-]*\+)?json(?:\s*;|$)/i.test(contentType || '');
  if (isJSON) {
    try {
      const value = typeof body === 'string' ? JSON.parse(body) : body;
      if (value && typeof value === 'object' && !Array.isArray(value)) {
        return { body: value, encoding: 'json' };
      }
    } catch (_) {}
    return null;
  }
  const entries = formEntries(body);
  return entries === null ? null : { body: entries, encoding: 'form' };
}
