// Shared request configuration for server-rendered interactions.
import { paratrackCSRF } from '/static/js/app-config.js';

document.addEventListener('htmx:configRequest', (event) => {
  const token = paratrackCSRF();
  if (token) event.detail.headers['X-CSRF-Token'] = token;
});
