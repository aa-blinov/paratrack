package web

import "testing"

// A spreadsheet decides a cell computes from its first character, so every
// user-entered name and note that starts like a formula must arrive
// neutralized, or opening the export runs something the person never wrote.
func TestCSVNeutralizeCell(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"=cmd|' /C calc'!A0", "'=cmd|' /C calc'!A0"},
		{"+SUM(A1:A9)", "'+SUM(A1:A9)"},
		{"-2+3|cmd", "'-2+3|cmd"},
		{"@import('x')", "'@import('x')"},
		{"\t=cmd", "'\t=cmd"},
		{"Обычное имя задачи", "Обычное имя задачи"},
		{"", ""},
		{"=HYPERLINK", "'=HYPERLINK"},
	}
	for _, test := range cases {
		if got := csvNeutralizeCell(test.in); got != test.want {
			t.Fatalf("csvNeutralizeCell(%q) = %q, want %q", test.in, got, test.want)
		}
	}
}
