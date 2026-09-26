// Package i18n provides the UI string dictionary and language
// resolution for the server-rendered pages.
//
// Dictionaries live as embedded JSON (en.json / ru.json) with flat
// dotted keys ("nav.dashboard"). English is the source of truth: if a
// key is missing from a locale the English string is used, and a
// missing English key falls back to the key itself so gaps are visible
// in development rather than blank.
package i18n

import (
	"embed"
	"encoding/json"
	"sync"
)

//go:embed dict/*.json
var dictFS embed.FS

// Lang is a supported UI language tag.
type Lang string

const (
	En Lang = "en"
	Ru Lang = "ru"
)

// Default is used when the visitor has no preference.
// Russian is the product's default language.
const Default = Ru

// Supported lists the language switcher options, in menu order.
var Supported = []Lang{En, Ru}

var (
	once sync.Once
	dict map[Lang]map[string]string
)

func load() {
	once.Do(func() {
		dict = map[Lang]map[string]string{}
		for _, l := range Supported {
			b, err := dictFS.ReadFile("dict/" + string(l) + ".json")
			if err != nil {
				dict[l] = map[string]string{}
				continue
			}
			m := map[string]string{}
			_ = json.Unmarshal(b, &m)
			dict[l] = m
		}
	})
}

// T returns the translation of key in lang, falling back to English
// and finally to the key itself.
func T(lang Lang, key string) string {
	load()
	if m, ok := dict[lang]; ok {
		if s, ok := m[key]; ok && s != "" {
			return s
		}
	}
	if m, ok := dict[En]; ok {
		if s, ok := m[key]; ok && s != "" {
			return s
		}
	}
	return key
}

// Normalize maps a user-supplied tag ("EN", "ru-RU") to a supported Lang.
func Normalize(s string) Lang {
	switch {
	case len(s) >= 2 && (s[0:2] == "ru" || s[0:2] == "RU"):
		return Ru
	case len(s) >= 2 && (s[0:2] == "en" || s[0:2] == "EN"):
		return En
	}
	return Default
}
