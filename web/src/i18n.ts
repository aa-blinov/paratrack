import en from "../../internal/i18n/dict/en.json"
import ru from "../../internal/i18n/dict/ru.json"

export type UiLanguage = "en" | "ru"

const dictionaries: Record<UiLanguage, Record<string, string>> = {
  en: en as Record<string, string>,
  ru: ru as Record<string, string>,
}

export function translate(language: string, key: string, ...values: Array<string | number>): string {
  const lang: UiLanguage = language === "en" ? "en" : "ru"
  let index = 0
  const text = dictionaries[lang][key] || dictionaries.en[key] || key
  return text.replace(/%[sd]/g, () => String(values[index++] ?? ""))
}

/**
 * The translation, or "" when the key has none.
 *
 * `translate` falls back to printing the key itself, which is right for a
 * missing string nobody renders deliberately — a wrong visible phrase is
 * better than a blank. It is wrong where the text is optional: a subtitle
 * that only some cards have would otherwise print "rep.by-day.blurb" to the
 * user. So optional text goes through here.
 */
export function translateOptional(language: string, key: string, ...values: Array<string | number>): string {
  const lang: UiLanguage = language === "en" ? "en" : "ru"
  if (!dictionaries[lang][key] && !dictionaries.en[key]) return ""
  return translate(language, key, ...values)
}
