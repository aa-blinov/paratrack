package translit

import "testing"

func TestLatin(t *testing.T) {
	for in, want := range map[string]string{
		"Сайт для Ромашки": "sait dlya romashki",
		"Щука и ЁЖ":        "shchuka i ezh",
		"EORA RAG":         "eora rag",
	} {
		if got := Latin(in); got != want {
			t.Errorf("Latin(%q) = %q, want %q", in, got, want)
		}
	}
}
