package i18n

import "testing"

func TestSupportedLanguagesReturnsCopy(t *testing.T) {
	languages := SupportedLanguages()
	if len(languages) == 0 {
		t.Fatal("SupportedLanguages returned no languages")
	}
	languages[0] = "changed"
	if got := SupportedLanguages()[0]; got != En {
		t.Fatalf("SupportedLanguages leaked mutable package state: first language = %q", got)
	}
}
