package model

import (
	"slices"
	"testing"
)

// The same cases the browser's builder is tested with, so both sides agree.
func TestRequisiteProblems(t *testing.T) {
	cases := []struct {
		text string
		want []string
	}{
		{"ООО Ромашка\nИНН 7707083893\nБИК 044525225, к/с 30101810400000000225", nil},
		{"ИП Иванов, ИНН: 500100732259", nil},
		{"ООО Ромашка, ИНН/КПП 7707083893/770701001", nil},
		{"ИНН/КПП 7707083894/770701001", []string{RequisiteINN}},
		{"ИП Иванов, ИНН/КПП 500100732259/", nil},
		{"ИП Иванов, ИНН/КПП 500100732259 / -", nil},
		{"ИНН 7707083894", []string{RequisiteINN}},
		{"БИК 044525225, к/с 30101810400000000226", []string{RequisiteCorr}},
		{"КПП 12345\nБИК 1234", []string{RequisiteKPP, RequisiteBIK}},
		{"Acme Ltd, 1 Main St, London", nil},
		{"", nil},
	}
	for _, c := range cases {
		if got := RequisiteProblems(c.text); !slices.Equal(got, c.want) {
			t.Errorf("RequisiteProblems(%q) = %v, want %v", c.text, got, c.want)
		}
	}
}

func TestParseRequisites(t *testing.T) {
	f := ParseRequisites("ИП Иванов Иван Иванович, ИНН/КПП 500100732259/\nр/с 4070 2810 9380 0000 0001 в ПАО Сбербанк, БИК 044525225\nк/с 30101810400000000225")
	want := RequisiteFields{Org: "ИП Иванов Иван Иванович", INN: "500100732259", Bank: "ПАО Сбербанк", BIK: "044525225", Corr: "30101810400000000225", Acc: "40702810938000000001"}
	if f != want {
		t.Fatalf("ParseRequisites = %+v, want %+v", f, want)
	}
}
