package db

import (
	"strings"
	"testing"
)

func TestTruncateUTF8(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		maxBytes int
		want     string
	}{
		{name: "within limit", value: "готово", maxBytes: 20, want: "готово"},
		{name: "zero limit", value: "текст", maxBytes: 0, want: ""},
		{name: "boundary in multibyte rune", value: strings.Repeat("a", 1999) + "💾", maxBytes: 2000, want: strings.Repeat("a", 1999)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := truncateUTF8(tt.value, tt.maxBytes); got != tt.want {
				t.Fatalf("truncateUTF8() length = %d, want %d", len(got), len(tt.want))
			}
		})
	}
}
