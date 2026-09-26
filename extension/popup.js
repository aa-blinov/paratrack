// paratrack browser extension — popup timer.
// Talks to the paratrack API with a Bearer token (Settings → API tokens).

const $ = (id) => document.getElementById(id);

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
  return res;
}

function setStatus(msg, el) {
  const node = el || $("setup").querySelector("#status") || $("app").querySelector("#status");
  if (node) node.textContent = msg;
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

  const active = await (await api("/api/active")).text();
  // The HTML fragment has sessions — parse lightly for name/start/paused.
  const row = active.match(/data-label="Activity"[\s\S]*?<span[^>]*>([^<]+)<\/span>/);
  const startISO = active.match(/x-data="liveDuration\('([^']+)'/);
  const paused = /is-paused/.test(active);
  const acc = (active.match(/liveDuration\('[^']+',\s*(\d+)/) || [])[1];
  const id = (active.match(/sessions\/(\d+)\/stop/) || [])[1];
  const name = row ? row[1].trim() : "";

  if (id && name) {
    $("state").textContent = paused ? "paused" : "active";
    $("state").className = "pill " + (paused ? "off" : "on");
    $("active-name").textContent = name;
    $("stop").dataset.id = id;
    $("pause").dataset.id = id;
    $("resume").dataset.id = id;
    tick({
      startISO: startISO ? startISO[1] : new Date().toISOString(),
      accumulated: Number(acc || 0),
      paused,
    });
  } else {
    $("state").textContent = "idle";
    $("state").className = "pill off";
    $("active-name").textContent = "";
    tick(null);
    delete $("stop").dataset.id;
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
  const r = await api("/api/start", { method: "POST", body });
  setStatus(r.ok ? "started " + name : "start failed (409 if already running)");
  $("activity").value = "";
  await refresh();
}

async function stopTimer() {
  const id = $("stop").dataset.id;
  if (!id) return;
  await api("/api/sessions/" + id + "/stop", { method: "POST" });
  setStatus("stopped");
  await refresh();
}

async function pauseTimer() {
  const id = $("pause").dataset.id;
  if (!id) return;
  await api("/api/sessions/" + id + "/pause", { method: "POST" });
  await refresh();
}

async function resumeTimer() {
  const id = $("resume").dataset.id;
  if (!id) return;
  await api("/api/sessions/" + id + "/resume", { method: "POST" });
  await refresh();
}

$("save").addEventListener("click", async () => {
  const base = $("base").value.trim() || "https://paratrack.duckdns.org";
  const token = $("token").value.trim();
  if (!token) { setStatus("token required"); return; }
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

// live tick while open
setInterval(() => {
  if ($("state").textContent === "active") {
    // cheap: recompute from the last tick() call state stored on the clock node
  }
}, 1000);

refresh().catch(() => {});
setInterval(() => { if (!$("app").classList.contains("hidden")) refresh().catch(() => {}); }, 15000);
