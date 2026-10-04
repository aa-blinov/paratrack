// Shared localized units for short duration labels in browser modules.
export function durationUnits() {
  return document.documentElement.lang === 'ru'
    ? ['\u00a0ч', '\u00a0мин', '\u00a0с']
    : ['h', 'm', 's'];
}
