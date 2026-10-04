// Track the last input modality so focus moves only when a keyboard user acts.
let keyboard = false;

document.addEventListener('keydown', (event) => {
  if (event.key === 'Tab' || event.key === 'Enter' || event.key === ' ') keyboard = true;
}, true);
document.addEventListener('pointerdown', () => { keyboard = false; }, true);

export function isKeyboardInputMode() {
  return keyboard;
}
