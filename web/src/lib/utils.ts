export { cn } from "cn"

// A share in the screen's own decimal mark: «50,5%» in Russian, not «50.5%»
// beside money that already reads «2 500,00 ₽».
export function formatShare(value: number, lang: string): string {
  return `${value.toLocaleString(lang === "ru" ? "ru-RU" : "en-US", { maximumFractionDigits: 1 })}%`
}
