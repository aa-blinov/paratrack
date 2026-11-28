// paratrack browser extension — popup timer.
// Talks to the paratrack API with a Bearer token (Settings → API tokens).

const $ = (id) => document.getElementById(id);

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
  const { base, token } = await loadCfg();
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
  if (res.status === 401) throw new Error("unauthorized");
  // A token that works but has no seat in its workspace any more: say so,
  // instead of reading the error body as "no timer running".
  if (res.status === 403) {
    let text = "";
    try { text = (await res.json()).error || ""; } catch (_) {}
    throw new Error("forbidden: " + (text || "no access to this workspace"));
  }
  return res;
}

function setStatus(msg, el) {
  const node = el || $("setup").querySelector("#status") || $("app").querySelector("#status");
  if (node) node.textContent = msg;
}

// The server explains a refusal in the body (localized), so the status line
// should say what actually happened rather than guess at the status code.
async function reportApiError(res, fallback) {
  let text = "";
  try { text = (await res.text()).trim(); } catch (_) {}
  setStatus(text || fallback + " (" + res.status + ")");
}

function fmt(sec) {
  sec = Math.max(0, Math.floor(sec));
  const m = Math.floor(sec / 60), s = sec % 60, h = Math.floor(m / 60);
  if (h > 0) return h + "h " + (m % 60) + "m";
  if (m > 0) return m + "m " + s + "s";
  return s + "s";
}

function tick(active) {
  if (!active) { $("clock").textContent = "0m"; return; }
  const started = new Date(active.startISO).getTime();
  const acc = active.accumulated || 0;
  const now = Date.now();
  const sec = active.paused ? acc : acc + (now - started) / 1000;
  $("clock").textContent = fmt(sec);
}

function clearTimer() {
  lastActive = null;
  $("state").textContent = "idle";
  $("state").className = "pill off";
  $("active-name").textContent = "";
  tick(null);
  delete $("stop").dataset.id;
  delete $("pause").dataset.id;
  delete $("resume").dataset.id;
}

async function refresh() {
  const { token } = await loadCfg();
  if (!token) {
    $("setup").classList.remove("hidden");
    $("app").classList.add("hidden");
    return;
  }
  $("setup").classList.add("hidden");
  $("app").classList.remove("hidden");

  try {
    const me = await (await api("/api/me")).json();
    $("who").textContent = (me.user && me.user.name) || "";
  } catch (e) {
    $("who").textContent = "";
    setStatus("bad token or server unreachable");
    return;
  }

  // /api/active answers JSON: one item per running session, the fields the
  // dashboard clock uses (ResumeISO anchors the live count, AccumulatedSeconds
  // is what the pauses have banked). It used to be parsed as an HTML fragment
  // with regular expressions, which never matched the JSON and left Stop,
  // Pause and Resume without a session id.
  let active;
  try {
    active = await (await api("/api/active")).json();
  } catch (e) {
    setStatus(String(e.message || "").startsWith("forbidden") ? e.message.replace(/^forbidden: /, "") : "server unreachable");
    return;
  }
  const items = Array.isArray(active.Items) ? active.Items : [];
  const current = items.find((x) => !x.Paused) || items[0];
  if (current && current.ID > 0 && current.ActivityName) {
    $("state").textContent = current.Paused ? "paused" : "active";
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

  // tasks dropdown
  try {
    const t = await (await api("/api/external-tasks")).json();
    const sel = $("task");
    sel.innerHTML = '<option value="">— pick a task —</option>';
    (t.tasks || []).forEach((x) => {
      const o = document.createElement("option");
      o.value = x.title;
      o.textContent = x.title;
      sel.appendChild(o);
    });
  } catch (_) {}
}

async function startTimer() {
  const task = $("task").value.trim();
  const act = $("activity").value.trim();
  const name = task || act;
  if (!name) { setStatus("pick a task or type an activity"); return; }
  const body = new URLSearchParams({ activity: name });
  try {
    const r = await api("/api/start", { method: "POST", body });
    if (!r.ok) { await reportApiError(r, "start failed"); return; }
    setStatus("started " + name);
    $("activity").value = "";
  } catch (e) {
    setStatus("server unreachable");
    return;
  }
  await refresh();
}

async function stopTimer() {
  const id = $("stop").dataset.id;
  if (!id) return;
  try {
    const r = await api("/api/sessions/" + id + "/stop", { method: "POST" });
    if (!r.ok) { await reportApiError(r, "stop failed"); }
    else setStatus("stopped");
  } catch (e) {
    setStatus("server unreachable");
  }
  await refresh();
}

async function pauseTimer() {
  const id = $("pause").dataset.id;
  if (!id) return;
  try {
    const r = await api("/api/sessions/" + id + "/pause", { method: "POST" });
    if (!r.ok) await reportApiError(r, "pause failed");
  } catch (e) {
    setStatus("server unreachable");
  }
  await refresh();
}

async function resumeTimer() {
  const id = $("resume").dataset.id;
  if (!id) return;
  try {
    const r = await api("/api/sessions/" + id + "/resume", { method: "POST" });
    if (!r.ok) await reportApiError(r, "resume failed");
  } catch (e) {
    setStatus("server unreachable");
  }
  await refresh();
}

$("save").addEventListener("click", async () => {
  const base = $("base").value.trim() || "https://paratrack.duckdns.org";
  const token = $("token").value.trim();
  if (!token) { setStatus("token required"); return; }
  // A self-hosted server needs its own host permission; ask for it here,
  // inside the click (Chrome only grants on a user gesture).
  let origin;
  try { origin = new URL(base).origin + "/*"; } catch (_) { setStatus("bad server address"); return; }
  const granted = await chrome.permissions.contains({ origins: [origin] }) ||
    await chrome.permissions.request({ origins: [origin] });
  if (!granted) { setStatus("access to " + origin + " was not granted"); return; }
  await chrome.storage.local.set({ base, token });
  try {
    await refresh();
    setStatus("connected");
  } catch (e) {
    setStatus("failed: " + e.message);
  }
});
$("start").addEventListener("click", startTimer);
$("stop").addEventListener("click", stopTimer);
$("pause").addEventListener("click", pauseTimer);
$("resume").addEventListener("click", resumeTimer);
$("refresh").addEventListener("click", refresh);

// live tick while open: recompute from the last known session instead of
// waiting for the next network refresh
setInterval(() => { if (lastActive) tick(lastActive); }, 1000);

refresh().catch(() => {});
setInterval(() => { if (!$("app").classList.contains("hidden")) refresh().catch(() => {}); }, 15000);
