const pad = (value: number) => String(value).padStart(2, "0")

/** The browser's wall clock in the YYYY-MM-DDTHH:MM form the date-time field uses. */
export function localDateTimeValue(date: Date): string {
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`
}
