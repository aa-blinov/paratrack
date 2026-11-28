// paratrack browser extension — popup timer.
// Talks to the paratrack API with a Bearer token (Settings → API tokens).

const $ = (id) => document.getElementById(id);

// Every text comes from _locales (Russian by default, as the app is); the
// key stands in when a message is missing.
const t = (key, ...subs) => (chrome.i18n && chrome.i18n.getMessage(key, subs.map(String))) || key;

function localize() {
  document.querySelectorAll("[data-i18n]").forEach((el) => { el.textContent = t(el.dataset.i18n); });
  document.querySelectorAll("[data-i18n-placeholder]").forEach((el) => { el.placeholder = t(el.dataset.i18nPlaceholder); });
}

// The session the popup is showing, as /api/active described it. The live
// clock recomputes from it every second instead of waiting for a refetch.
let lastActive = null;

async function loadCfg() {
  const cfg = await chrome.storage.local.get(["base", "token"]);
  return {
    base: (cfg.base || "https://paratrack.duckdns.org").replace(/\/$/, ""),
    token: cfg.token || "",
  };
}

async function api(path, opts = {}) {
  return apiWith(await loadCfg(), path, opts);
}

// apiWith calls the server with a given address and token, so a token can be
// tried before it is saved.
async function apiWith({ base, token }, path, opts = {}) {
  if (!token) throw new Error("no token");
  const res = await fetch(base + path, {
    ...opts,
    headers: {
      Authorization: "Bearer " + token,
      ...(opts.body instanceof URLSearchParams
        ? { "Content-Type": "application/x-www-form-urlencoded" }
        : {}),
      ...(opts.headers || {}),
    },
    credentials: "omit",
  });
  // A refused token (401) or one with no seat in its workspace or no right
  // to write (403) is not "server unreachable" and not "no timer": every
  // caller shows the server's own words, through explain().
  if (res.status === 401 || res.status === 403) {
    throw new ApiError(res.status, await refusalText(res));
  }
  // The server answered, but with an error page: that is not "unreachable".
  if (res.status >= 500) {
    throw new ApiError(res.status, t("serverError", res.status));
  }
  return res;
}

// readJSON is a successful paratrack answer or an ApiError saying what came
// back instead: a wrong server address answers 404 or an HTML page, and that
// is "check the address", not "unreachable".
async function readJSON(res) {
  if (!res.ok) throw new ApiError(res.status, t("notParatrack", res.status));
  try {
    return await res.json();
  } catch (_) {
    throw new ApiError(res.status, t("notParatrack", res.status));
  }
}

class ApiError extends Error {
  constructor(status, message) {
    super(message);
    this.status = status;
  }
}

// The server answers a refusal as JSON {"error": …} or as plain text.
async function refusalText(res) {
  let text = "";
  try { text = (await res.text()).trim(); } catch (_) {}
  try { const parsed = JSON.parse(text); if (parsed && parsed.error) text = parsed.error; } catch (_) {}
  if (text) return text;
  return res.status === 401 ? t("tokenNotAccepted") : t("noAccess");
}

// explain turns what a call threw into the status line: the server's reason
// for a refusal, and "server unreachable" only when nothing answered.
function explain(e) {
  if (e instanceof ApiError) return e.message;
  if (e && e.message === "no token") return t("tokenRequired");
  return t("unreachable");
}

// setStatus writes into the section on screen: the setup and the timer each
// have their own line, and a message in the hidden one is never seen.
function setStatus(msg) {
  const node = $("app").classList.contains("hidden") ? $("setup-status") : $("app-status");
  node.textContent = msg;
}

// showSetup opens the settings with what is saved filled in, and a way back
// to the timer while the saved token still works.
async function showSetup() {
  const { base, token } = await loadCfg();
  $("base").value = base;
  $("token").value = "";
  $("token").placeholder = token ? t("tokenSaved") : "pt_…";
  $("back").classList.toggle("hidden", !token);
  $("setup").classList.remove("hidden");
  $("app").classList.add("hidden");
}

// The server explains a refusal in the body (localized), so the status line
// should say what actually happened rather than guess at the status code.
// The same parser as api()'s refusals: JSON {"error": …} or plain text.
async function reportApiError(res) {
  let text = "";
  try { text = (await res.text()).trim(); } catch (_) {}
  try { const parsed = JSON.parse(text); if (parsed && parsed.error) text = parsed.error; } catch (_) {}
  setStatus(text || t("failed", res.status));
}

function fmt(sec) {
  sec = Math.max(0, Math.floor(sec));
  const m = Math.floor(sec / 60), s = sec % 60, h = Math.floor(m / 60);
  if (h > 0) return t("clockHM", h, m % 60);
  if (m > 0) return t("clockMS", m, s);
  return t("clockS", s);
}

function tick(active) {
  if (!active) { $("clock").textContent = t("clockM0"); return; }
  const started = new Date(active.startISO).getTime();
  const acc = active.accumulated || 0;
  const now = Date.now();
  const sec = active.paused ? acc : acc + (now - started) / 1000;
  $("clock").textContent = fmt(sec);
}

function clearTimer() {
  lastActive = null;
  $("state").textContent = t("idle");
  $("state").className = "pill off";
  $("active-name").textContent = "";
  tick(null);
  delete $("stop").dataset.id;
  delete $("pause").dataset.id;
  delete $("resume").dataset.id;
}

// refresh redraws the popup and says whether the server accepted the token,
// so the settings screen never prints "connected" over a refusal.
async function refresh() {
  const { token } = await loadCfg();
  if (!token) {
    showSetup();
    return false;
  }

  try {
    const me = await readJSON(await api("/api/me"));
    $("who").textContent = (me.user && me.user.name) || "";
  } catch (e) {
    // The saved token stopped working (revoked, removed from the workspace):
    // back to the setup with the reason, not an empty timer screen. A network
    // blip is not that: the timer screen stays and says the server is not
    // answering, and the next automatic refresh tries again.
    clearTimer();
    if (e instanceof ApiError && (e.status === 401 || e.status === 403)) {
      $("who").textContent = "";
      showSetup();
    }
    setStatus(explain(e));
    return false;
  }
  $("setup").classList.add("hidden");
  $("app").classList.remove("hidden");

  // /api/active answers JSON: one item per running session, the fields the
  // dashboard clock uses (ResumeISO anchors the live count, AccumulatedSeconds
  // is what the pauses have banked). It used to be parsed as an HTML fragment
  // with regular expressions, which never matched the JSON and left Stop,
  // Pause and Resume without a session id.
  let active;
  try {
    active = await readJSON(await api("/api/active"));
  } catch (e) {
    clearTimer();
    setStatus(explain(e));
    return false;
  }
  const items = Array.isArray(active.Items) ? active.Items : [];
  // Several timers can run at once; the buttons act on the one picked from
  // the list, the running one first.
  const current = items.find((x) => x.ID === pickedID) || items.find((x) => !x.Paused) || items[0];
  renderOthers(items.filter((x) => current && x.ID !== current.ID));
  if (current && current.ID > 0 && current.ActivityName) {
    $("state").textContent = current.Paused ? t("paused") : t("active");
    $("state").className = "pill " + (current.Paused ? "off" : "on");
    $("active-name").textContent = current.ActivityName;
    $("stop").dataset.id = current.ID;
    $("pause").dataset.id = current.ID;
    $("resume").dataset.id = current.ID;
    lastActive = {
      startISO: current.ResumeISO || current.StartISO,
      accumulated: Number(current.AccumulatedSeconds || 0),
      paused: current.Paused,
    };
    tick(lastActive);
  } else {
    clearTimer();
  }

  // Tasks from the connected trackers. Not having any is normal (no
  // integration), so a failure here leaves the blank choice and says nothing;
  // the list is replaced only once a new one has arrived.
  try {
    const taskList = await readJSON(await api("/api/external-tasks"));
    const sel = $("task");
    sel.innerHTML = "";
    const blank = document.createElement("option");
    blank.value = "";
    blank.textContent = t("pickTask");
    sel.appendChild(blank);
    (taskList.tasks || []).forEach((x) => {
      const o = document.createElement("option");
      o.value = x.title;
      o.textContent = x.title;
      sel.appendChild(o);
    });
  } catch (_) {}
  return true;
}

async function startTimer() {
  const task = $("task").value.trim();
  const act = $("activity").value.trim();
  const name = task || act;
  if (!name) { setStatus(t("pickOrType")); return; }
  const body = new URLSearchParams({ activity: name });
  try {
    const r = await api("/api/start", { method: "POST", body });
    if (!r.ok) { await reportApiError(r); return; }
    setStatus(t("started", name));
    $("activity").value = "";
  } catch (e) {
    setStatus(explain(e));
    return;
  }
  await refresh();
  notifyBadge();
}

async function stopTimer() {
  const id = $("stop").dataset.id;
  if (!id) return;
  try {
    const r = await api("/api/sessions/" + id + "/stop", { method: "POST" });
    if (!r.ok) { await reportApiError(r); }
    else setStatus(t("stopped"));
  } catch (e) {
    setStatus(explain(e));
  }
  await refresh();
  notifyBadge();
}

async function pauseTimer() {
  const id = $("pause").dataset.id;
  if (!id) return;
  try {
    const r = await api("/api/sessions/" + id + "/pause", { method: "POST" });
    if (!r.ok) await reportApiError(r);
  } catch (e) {
    setStatus(explain(e));
  }
  await refresh();
  notifyBadge();
}

async function resumeTimer() {
  const id = $("resume").dataset.id;
  if (!id) return;
  try {
    const r = await api("/api/sessions/" + id + "/resume", { method: "POST" });
    if (!r.ok) await reportApiError(r);
  } catch (e) {
    setStatus(explain(e));
  }
  await refresh();
  notifyBadge();
}

$("save").addEventListener("click", async () => {
  const base = $("base").value.trim() || "https://paratrack.duckdns.org";
  const token = $("token").value.trim();
  if (!token) { setStatus(t("tokenRequired")); return; }
  // A self-hosted server needs its own host permission; ask for it here,
  // inside the click (Chrome only grants on a user gesture).
  let origin;
  try { origin = new URL(base).origin + "/*"; } catch (_) { setStatus(t("badAddress")); return; }
  const granted = await chrome.permissions.contains({ origins: [origin] }) ||
    await chrome.permissions.request({ origins: [origin] });
  if (!granted) { setStatus(t("notGranted", origin)); return; }
  // The token is tried first and saved only when the server takes it: a
  // dead token never replaces a working one or hides the setup screen.
  try {
    await readJSON(await apiWith({ base: base.replace(/\/$/, ""), token }, "/api/me"));
  } catch (e) {
    setStatus(explain(e));
    return;
  }
  await chrome.storage.local.set({ base, token });
  if (await refresh()) setStatus(t("connected"));
});
$("start").addEventListener("click", startTimer);
$("stop").addEventListener("click", stopTimer);
$("pause").addEventListener("click", pauseTimer);
$("resume").addEventListener("click", resumeTimer);
$("refresh").addEventListener("click", refresh);
$("settings").addEventListener("click", showSetup);
$("back").addEventListener("click", () => { $("setup").classList.add("hidden"); $("app").classList.remove("hidden"); refresh(); });

localize();

// live tick while open: recompute from the last known session instead of
// waiting for the next network refresh
setInterval(() => { if (lastActive) tick(lastActive); }, 1000);

refresh().catch(() => {});
setInterval(() => { if (!$("app").classList.contains("hidden")) refresh().catch(() => {}); }, 15000);

// pickedID is the timer the controls act on when several run.
let pickedID = 0;

// renderOthers lists the other timers under the clock; picking one makes the
// controls act on it.
function renderOthers(others) {
  const list = $("others");
  list.innerHTML = "";
  list.classList.toggle("hidden", others.length === 0);
  others.forEach((x) => {
    const li = document.createElement("li");
    const button = document.createElement("button");
    button.className = "link";
    const started = new Date(x.ResumeISO || x.StartISO).getTime();
    const sec = x.Paused ? Number(x.AccumulatedSeconds || 0) : Number(x.AccumulatedSeconds || 0) + (Date.now() - started) / 1000;
    button.textContent = x.ActivityName + " — " + fmt(sec) + (x.Paused ? " (" + t("paused") + ")" : "");
    button.addEventListener("click", () => { pickedID = x.ID; refresh(); });
    li.appendChild(button);
    list.appendChild(li);
  });
}

// notifyBadge asks the background worker to redraw the toolbar badge after
// a timer changed here.
function notifyBadge() {
  try {
    if (typeof chrome !== "undefined" && chrome.runtime && chrome.runtime.sendMessage) {
      chrome.runtime.sendMessage({ type: "refresh-badge" }).catch?.(() => {});
    }
  } catch (_) {}
}
