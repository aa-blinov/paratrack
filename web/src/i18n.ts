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
