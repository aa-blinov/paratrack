import { translate } from "@/i18n"

type Field = HTMLInputElement | HTMLTextAreaElement | HTMLSelectElement

// The browser words its validation bubble in the browser's language, which is
// often not the app's. One listener sets the same checks in the page's own
// language. A field that becomes valid again drops the message at once.
export function installFormMessages() {
  document.addEventListener("invalid", (event) => {
    const field = event.target
    if (!isField(field)) return
    field.setCustomValidity(validationMessage(field))
  }, true)
  for (const type of ["input", "change"]) {
    document.addEventListener(type, (event) => {
      const field = event.target
      if (isField(field)) field.setCustomValidity("")
    }, true)
  }
}

function isField(target: EventTarget | null): target is Field {
  return target instanceof HTMLInputElement || target instanceof HTMLTextAreaElement || target instanceof HTMLSelectElement
}

function validationMessage(field: Field): string {
  const lang = document.documentElement.lang === "en" ? "en" : "ru"
  const state = field.validity
  if (state.valueMissing) return translate(lang, "form.valueMissing")
  if (state.typeMismatch && field instanceof HTMLInputElement) {
    if (field.type === "email") return translate(lang, "form.typeEmail")
    if (field.type === "url") return translate(lang, "form.typeUrl")
    return translate(lang, "form.typeMismatch")
  }
  if (state.tooShort && field instanceof HTMLInputElement) return translate(lang, "form.tooShort", field.minLength)
  if (state.tooShort && field instanceof HTMLTextAreaElement) return translate(lang, "form.tooShort", field.minLength)
  if (state.rangeUnderflow && field instanceof HTMLInputElement) return translate(lang, "form.rangeUnderflow", field.min)
  if (state.rangeOverflow && field instanceof HTMLInputElement) return translate(lang, "form.rangeOverflow", field.max)
  if (state.stepMismatch && field instanceof HTMLInputElement) return translate(lang, "form.stepMismatch", field.step)
  if (state.badInput) return translate(lang, "form.badInput")
  return translate(lang, "form.patternMismatch")
}
