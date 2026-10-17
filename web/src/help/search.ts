// Help search: every word of the query must appear in the entry. Case does not
// matter, and «ё» counts as «е», so «счет» finds «счёт» and «ПРИГЛАШЕНИЕ» finds
// «приглашение». An empty query matches everything, so the page starts whole.
export function normalizeHelpText(text: string): string {
  return text.toLowerCase().replace(/ё/g, "е")
}

export function matchesHelpQuery(entry: string, query: string): boolean {
  const words = normalizeHelpText(query).split(/\s+/).filter(Boolean)
  if (words.length === 0) return true
  const haystack = normalizeHelpText(entry)
  return words.every(word => haystack.includes(word))
}
