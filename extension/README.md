# paratrack browser extension (MV3)

Popup timer: start / pause / resume / stop, pick an imported GitHub/Trello
task or type an activity name. Authenticates with a Bearer API token.

## Install (Chrome / Chromium)

1. paratrack → Settings → API tokens → Create token → copy `pt_…`
2. `chrome://extensions` → Developer mode → Load unpacked → pick this folder
3. Click the toolbar icon → paste Server URL + token → Save & connect

## Notes

- Uses `Authorization: Bearer pt_…` against `/api/me`, `/api/active`,
  `/api/start`, `/api/sessions/{id}/…`, `/api/external-tasks`.
- Token is stored in `chrome.storage.local` (per browser profile).
- `manifest.json` `host_permissions` must list every paratrack origin you use.
