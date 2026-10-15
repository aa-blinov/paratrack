export type UiLanguage = "en" | "ru"

// One language per page: both dictionaries used to ride in the render-blocking
// bundle, about 44 KB gzipped of which a person reads half. The page loader
// awaits the language it is about to show; the two files hold the same keys.
const dictionaries: Partial<Record<UiLanguage, Record<string, string>>> = {}

const asLanguage = (language: string): UiLanguage => language === "en" ? "en" : "ru"

export async function loadDictionary(language: string): Promise<void> {
  const lang = asLanguage(language)
  if (dictionaries[lang]) return
  const module = lang === "en"
    ? await import("../../internal/i18n/dict/en.json")
    : await import("../../internal/i18n/dict/ru.json")
  dictionaries[lang] = module.default as Record<string, string>
}

function lookup(language: string, key: string): string | undefined {
  const dictionary = dictionaries[asLanguage(language)] ?? dictionaries.ru ?? dictionaries.en
  return dictionary?.[key]
}

export function translate(language: string, key: string, ...values: Array<string | number>): string {
  let index = 0
  const text = lookup(language, key) || key
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
  if (!lookup(language, key)) return ""
  return translate(language, key, ...values)
}
