// ---------------------------------------------------------------------------
// HTMX progress feedback and delayed skeletons for async page regions.
// Loading feedback (Wave 10): a thin top progress bar for every HTMX request,
// and skeleton rows in the list regions while their fragment is in flight.
// Server-rendered pages arrive complete — skeletons are only for the async
// regions where there is a real wait.
// ---------------------------------------------------------------------------
(function () {
  const REGION_SKEL = {
    '#active-list': '<div class="sk sk-row"></div><div class="sk sk-row"></div>',
    '#goals-list':  '<div class="sk sk-row"></div><div class="sk sk-row"></div>',
    '#tags-list':   '<div class="sk sk-line w-70"></div><div class="sk sk-line w-50"></div>',
  };

  function bar() {
    let el = document.getElementById('htmx-progress');
    if (!el) {
      el = document.createElement('div');
      el.id = 'htmx-progress';
      document.body.appendChild(el);
    }
    return el;
  }
  // Both the progress bar and the region skeletons are deferred: a fast
  // request (the common case) must NOT flash
  // a skeleton over content the user is already reading.
  const GRACE = 180; // ms before loading feedback appears
  let barTimer = null;

  function showBar() {
    clearTimeout(barTimer);
    barTimer = setTimeout(() => {
      const el = bar();
      el.style.transform = 'scaleX(.15)';
      el.classList.add('on');
      el.style.transform = 'scaleX(.7)';
    }, GRACE);
  }
  function hideBar() {
    clearTimeout(barTimer);
    const el = bar();
    if (!el.classList.contains('on')) { el.style.transform = 'scaleX(0)'; return; }
    el.style.transform = 'scaleX(1)';
    setTimeout(() => {
      el.classList.remove('on');
      el.style.transform = 'scaleX(0)';
    }, 160);
  }

  const saved = new WeakMap();
  const skelTimers = new WeakMap();

  document.body.addEventListener('htmx:beforeRequest', (e) => {
    showBar();
    const t = e.detail && e.detail.target;
    if (!t || !t.id) return;
    const sk = REGION_SKEL['#' + t.id];
    if (!sk) return;
    saved.set(t, t.innerHTML);
    skelTimers.set(t, setTimeout(() => {
      // still waiting after GRACE — now the skeleton is honest
      if (saved.has(t)) t.innerHTML = sk;
    }, GRACE));
  });

  document.body.addEventListener('htmx:afterRequest', (e) => {
    hideBar();
    const t = e.detail && e.detail.target;
    if (!t) return;
    const timer = skelTimers.get(t);
    if (timer) { clearTimeout(timer); skelTimers.delete(t); }
    if (saved.has(t)) {
      if (e.detail.xhr && e.detail.xhr.status >= 400) {
        t.innerHTML = saved.get(t);
      }
      saved.delete(t);
    }
  });

  document.body.addEventListener('htmx:sendError', hideBar);
  document.body.addEventListener('htmx:responseError', hideBar);
})();
