// Package translit turns Cyrillic into Latin for URL slugs
// ("Сайт для Ромашки" → "sait dlya romashki"), so a Russian name doesn't
// collapse into a generic fallback slug.
package translit

import "strings"

var table = map[rune]string{
	'а': "a", 'б': "b", 'в': "v", 'г': "g", 'д': "d", 'е': "e", 'ё': "e", 'ж': "zh",
	'з': "z", 'и': "i", 'й': "i", 'к': "k", 'л': "l", 'м': "m", 'н': "n", 'о': "o",
	'п': "p", 'р': "r", 'с': "s", 'т': "t", 'у': "u", 'ф': "f", 'х': "kh", 'ц': "ts",
	'ч': "ch", 'ш': "sh", 'щ': "shch", 'ъ': "", 'ы': "y", 'ь': "", 'э': "e", 'ю': "yu",
	'я': "ya", 'і': "i", 'ї': "yi", 'є': "ye", 'ґ': "g", 'ў': "u", 'қ': "k", 'ғ': "g",
	'ү': "u", 'ұ': "u", 'ң': "n", 'ө': "o", 'һ': "h", 'ә': "a",
}

// Latin lowercases s and replaces Cyrillic letters; other runes pass.
func Latin(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if t, ok := table[r]; ok {
			b.WriteString(t)
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
