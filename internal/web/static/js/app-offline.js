// Queue offline mutations and replay them when the connection returns.
import { paratrackCSRF, paratrackT } from '/static/js/app-config.js';
import { createOfflineBanner } from '/static/js/app-offline-banner.js';
import { isOfflineQueueableMutation } from '/static/js/app-offline-policy.js';
import {
  encodeOfflineRequestPayload,
  isValidOfflineQueueItem,
  readOfflineQueue,
  sameOriginPath,
  writeOfflineQueue,
} from '/static/js/app-offline-queue.js';
import { paratrackToast } from '/static/js/app-toast.js';

let offlineFetch;

export class OfflineQueuedError extends Error {
  constructor() {
    super('request queued while offline');
    this.name = 'OfflineQueuedError';
  }
}

export function apiFetch(...args) {
  if (!offlineFetch) throw new Error('offline-aware fetch is not initialized');
  return offlineFetch(...args);
}
// ---------------------------------------------------------------------------
// Offline mode (Wave 9): banner + mutation queue flushed on reconnect.
// Queue items carry the request BODY — a replay without it is a 400 that
// silently drops the user's change. Only 2xx dequeues.
// ---------------------------------------------------------------------------
(function () {
  const KEY = "paratrack-offline-queue";
  let storage = null;
  try { storage = window.localStorage; } catch (_) {}
  let blockedStatus = null;
  let flushing = false;
  const offlineBanner = createOfflineBanner((action) => {
    if (action === "retry") {
      blockedStatus = null;
      flush();
      return;
    }
    const pending = queue();
    pending.shift();
    if (!save(pending)) {
      blockedStatus = 0;
      render();
      return;
    }
    blockedStatus = null;
    render();
    flush();
  });

  function queue() {
    return storage ? readOfflineQueue(storage, KEY) : [];
  }
  function save(q) {
    return storage !== null && writeOfflineQueue(storage, KEY, q);
  }

  const T = paratrackT;

  function render() {
    offlineBanner.render({ online: navigator.onLine, queueLength: queue().length, blockedStatus });
  }

  function enqueue(item) {
    const q = queue();
    q.push(item);
    if (!save(q)) return false;
    render();
    paratrackToast(T.queued, "warning", 4000);
    return true;
  }

  function enqueueRequest(method, rawURL, body, contentType) {
    const url = sameOriginPath(rawURL, window.location.href);
    const verb = String(method || "").toUpperCase();
    if (!url || !isOfflineQueueableMutation(verb, new URL(url, window.location.href).pathname)) return false;
    const payload = encodeOfflineRequestPayload(body, contentType);
    if (!payload) return false;
    return enqueue({ verb, url, ...payload, ts: Date.now() });
  }

  // Queue mutating HTMX requests that fire while the network is down.
  document.body.addEventListener("htmx:beforeRequest", (e) => {
    if (navigator.onLine) return;
    const el = e.detail.elt;
    if (!el) return;
    const verb = (el.getAttribute("hx-post") && "POST") ||
                 (el.getAttribute("hx-delete") && "DELETE") ||
                 (el.getAttribute("hx-patch") && "PATCH") ||
                 (el.getAttribute("hx-put") && "PUT") || "";
    if (!verb) return;
    const cfg = e.detail.requestConfig || {};
    const params = cfg.parameters || cfg.formData || cfg.unfilteredFormData;
    const url = el.getAttribute("hx-post") || el.getAttribute("hx-delete") ||
                el.getAttribute("hx-patch") || el.getAttribute("hx-put");
    if (enqueueRequest(verb, url, params, "application/x-www-form-urlencoded")) e.preventDefault();
  });

  // Same for manual mutations such as session edits and push controls.
  const origFetch = window.fetch.bind(window);
  offlineFetch = async function (...args) {
    const req = args[0];
    const opts = args[1] || {};
    const method = (opts.method || (req && req.method) || "GET").toUpperCase();
    let replayRequest = null;
    if (method !== "GET" && req instanceof Request) {
      try { replayRequest = req.clone(); } catch (_) {}
    }
    try {
      return await origFetch(...args);
    } catch (err) {
      if (method !== "GET" && !navigator.onLine) {
        const url = typeof req === "string" ? req : (req && req.url) || "";
        const headers = new Headers(opts.headers || (req instanceof Request ? req.headers : undefined));
        let body = opts.body;
        if (body === undefined && req instanceof Request) {
          if (!replayRequest) throw err;
          try { body = await replayRequest.text(); } catch (_) { throw err; }
        }
        if (enqueueRequest(method, url, body, headers.get("Content-Type") || "")) {
          throw new OfflineQueuedError();
        }
      }
      throw err;
    }
  };

  async function flush() {
    if (flushing) return;
    if (!navigator.onLine) { render(); return; }
    if (!queue().length) { render(); return; }
    flushing = true;
    const csrf = paratrackCSRF();
    try {
      while (navigator.onLine) {
        const q = queue();
        if (!q.length) break;
        // Confirm storage still accepts writes before sending a mutation. If
        // local persistence is unavailable, leave the durable queue untouched.
        if (!save(q)) {
          blockedStatus = 0;
          break;
        }
        const item = q[0];
        if (!isValidOfflineQueueItem(item)) {
          q.shift();
          if (!save(q)) {
            blockedStatus = 0;
            break;
          }
          continue;
        }
        const signature = JSON.stringify(item);
        const url = sameOriginPath(item.url, window.location.href);
        const pathname = url ? new URL(url, window.location.href).pathname : "";
        if (!url || !isOfflineQueueableMutation(item.verb, pathname)) {
          // Purge records created by older clients before the replay allowlist
          // existed, including credential-bearing mutations.
          q.shift();
          if (!save(q)) {
            blockedStatus = 0;
            break;
          }
          continue;
        }
        const isJSON = item.encoding === "json";
        const body = isJSON ? JSON.stringify(item.body || {}) : new URLSearchParams(item.body || {});
        if (!isJSON && !body.has("csrf_token")) body.set("csrf_token", csrf);
        // Timer actions happen at click time, not at reconnect time.
        if (item.ts && !isJSON) body.set("client_ts", String(item.ts));
        let res;
        try {
          res = await origFetch(url, {
            method: item.verb,
            credentials: "same-origin",
            headers: {
              "X-CSRF-Token": csrf,
              "HX-Request": "true",
              "Content-Type": isJSON ? "application/json" : "application/x-www-form-urlencoded",
            },
            body,
          });
        } catch (_) {
          blockedStatus = 0;
          break; // still offline — keep the rest
        }
        // HTTP 4xx/5xx is NOT a successful replay: keep the item available
        // for correction, explicit retry or removal.
        if (!res.ok) {
          blockedStatus = res.status;
          break;
        }
        const latest = queue();
        if (JSON.stringify(latest[0]) !== signature) break;
        latest.shift();
        if (!save(latest)) {
          blockedStatus = 0;
          break;
        }
      }
    } finally {
      flushing = false;
    }
    render();
    // refresh active list / page state after sync
    if (window.htmx) window.htmx.trigger(document.body, "paratrack:synced");
  }

  window.addEventListener("online", () => { render(); flush(); });
  window.addEventListener("offline", render);
  document.addEventListener("DOMContentLoaded", render);
  render();
  if (navigator.onLine) flush();
})();
