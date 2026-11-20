// The offline fallback keeps no inline script or handler: the app's CSP
// allows scripts only from this origin, and this file is that origin's.
// The service worker precaches it with the offline page itself.
document.addEventListener("DOMContentLoaded", () => {
  const retry = document.getElementById("offline-retry")
  if (retry) retry.addEventListener("click", () => location.reload())
})
window.addEventListener("online", () => location.reload())
