package model

import (
	"regexp"
	"strings"
)

// Russian company and bank details are stored as free text. These checks
// read the numbers out of that text by their labels and say which of them
// cannot be right. A number that is not there is not a problem: not every
// document needs every number, and foreign details have none of them. The
// browser's builder (web/src/settings/requisites.ts) runs the same rules, so
// a number it accepts the server accepts too.

// Requisite fields, in the order they are reported.
const (
	RequisiteINN  = "inn"
	RequisiteKPP  = "kpp"
	RequisiteOGRN = "ogrn"
	RequisiteBIK  = "bik"
	RequisiteAcc  = "acc"
	RequisiteCorr = "corr"
)

const requisiteNumber = `\s*[:№]?\s*(\d(?:[\d ]*\d)?)`

var requisitePatterns = []struct {
	field string
	re    *regexp.Regexp
}{
	{RequisiteINN, regexp.MustCompile(`(?i)ИНН` + requisiteNumber)},
	{RequisiteKPP, regexp.MustCompile(`(?i)КПП` + requisiteNumber)},
	{RequisiteOGRN, regexp.MustCompile(`(?i)ОГРН(?:ИП)?(?:\s*/\s*ОГРНИП)?` + requisiteNumber)},
	{RequisiteAcc, regexp.MustCompile(`(?i)р/с` + requisiteNumber)},
	{RequisiteBIK, regexp.MustCompile(`(?i)БИК` + requisiteNumber)},
	{RequisiteCorr, regexp.MustCompile(`(?i)к/с` + requisiteNumber)},
}

// requisiteINNKPP reads the "ИНН/КПП 7707083893/770701001" pair 1C prints on
// every invoice; read field by field, the КПП label would take the ИНН.
// A sole trader has no КПП, and 1C leaves it empty or prints a dash:
// "ИНН/КПП 500100732259/" or "ИНН/КПП 500100732259 / -".
var requisiteINNKPP = regexp.MustCompile(`(?i)ИНН\s*/\s*КПП\s*[:№]?\s*(\d+)\s*/\s*(\d+|-)?`)

// RequisiteProblems lists the labelled numbers in text that fail their
// length or control-digit check, in a fixed order.
func RequisiteProblems(text string) []string {
	found := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		if m := requisiteINNKPP.FindStringSubmatch(line); m != nil {
			if _, ok := found[RequisiteINN]; !ok {
				found[RequisiteINN] = m[1]
			}
			if _, ok := found[RequisiteKPP]; !ok && m[2] != "" && m[2] != "-" {
				found[RequisiteKPP] = m[2]
			}
			// Cut out, or the КПП label would read the ИНН as a КПП.
			line = strings.Replace(line, m[0], " ", 1)
		}
		for _, p := range requisitePatterns {
			if _, ok := found[p.field]; ok {
				continue
			}
			if m := p.re.FindStringSubmatch(line); m != nil {
				found[p.field] = strings.ReplaceAll(m[1], " ", "")
			}
		}
	}
	var bad []string
	digitsOf := func(s string, lengths ...int) bool {
		for _, r := range s {
			if r < '0' || r > '9' {
				return false
			}
		}
		for _, n := range lengths {
			if len(s) == n {
				return true
			}
		}
		return false
	}
	if v, ok := found[RequisiteINN]; ok && !innValid(v) {
		bad = append(bad, RequisiteINN)
	}
	if v, ok := found[RequisiteKPP]; ok && !digitsOf(v, 9) {
		bad = append(bad, RequisiteKPP)
	}
	if v, ok := found[RequisiteOGRN]; ok && !digitsOf(v, 13, 15) {
		bad = append(bad, RequisiteOGRN)
	}
	bik, hasBIK := found[RequisiteBIK]
	bikOK := hasBIK && digitsOf(bik, 9)
	if hasBIK && !bikOK {
		bad = append(bad, RequisiteBIK)
	}
	if v, ok := found[RequisiteAcc]; ok && (!digitsOf(v, 20) || (bikOK && !accountKeyValid(bik[6:], v))) {
		bad = append(bad, RequisiteAcc)
	}
	if v, ok := found[RequisiteCorr]; ok && (!digitsOf(v, 20) || (bikOK && !accountKeyValid("0"+bik[4:6], v))) {
		bad = append(bad, RequisiteCorr)
	}
	return bad
}

func innValid(inn string) bool {
	d := make([]int, 0, len(inn))
	for _, r := range inn {
		if r < '0' || r > '9' {
			return false
		}
		d = append(d, int(r-'0'))
	}
	check := func(weights []int) int {
		sum := 0
		for i, w := range weights {
			sum += w * d[i]
		}
		return sum % 11 % 10
	}
	switch len(d) {
	case 10:
		return check([]int{2, 4, 10, 3, 5, 9, 4, 6, 8}) == d[9]
	case 12:
		return check([]int{7, 2, 4, 10, 3, 5, 9, 4, 6, 8}) == d[10] &&
			check([]int{3, 7, 2, 4, 10, 3, 5, 9, 4, 6, 8}) == d[11]
	}
	return false
}

// accountKeyValid checks a Russian account's control key against the БИК
// part in front of it, weighted 7-1-3.
func accountKeyValid(prefix, account string) bool {
	weights := [3]int{7, 1, 3}
	sum := 0
	for i, r := range prefix + account {
		sum += int(r-'0') * weights[i%3]
	}
	return sum%10 == 0
}

// NormalizeDetails is how details are stored: LF line endings, no trailing
// space. A textarea sends CRLF, and comparing or storing both forms made the
// same details look changed.
func NormalizeDetails(text string) string {
	return strings.TrimSpace(strings.ReplaceAll(text, "\r\n", "\n"))
}

// RequisitesError names the numbers that do not check out, on the seller's
// side (the workspace) or the client's.
type RequisitesError struct {
	Client bool
	Fields []string
}

func (e *RequisitesError) Error() string {
	side := "seller"
	if e.Client {
		side = "client"
	}
	return side + " details do not check out: " + strings.Join(e.Fields, ", ")
}
