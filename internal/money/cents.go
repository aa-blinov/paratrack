package money

import (
	"errors"
	"strconv"
	"strings"
)

var ErrInvalidAmount = errors.New("invalid non-negative decimal amount")

// ParseCents converts a decimal amount to cents without passing through
// floating point. Extra decimal places are rounded half up.
func ParseCents(value string) (int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, ErrInvalidAmount
	}
	if value[0] == '+' {
		value = value[1:]
	}
	if value == "" || strings.HasPrefix(value, "-") {
		return 0, ErrInvalidAmount
	}
	wholeText, fractionText, hasDecimal := strings.Cut(value, ".")
	if !hasDecimal {
		wholeText = value
	}
	if wholeText == "" {
		return 0, ErrInvalidAmount
	}
	for _, digit := range wholeText {
		if digit < '0' || digit > '9' {
			return 0, ErrInvalidAmount
		}
	}
	for _, digit := range fractionText {
		if digit < '0' || digit > '9' {
			return 0, ErrInvalidAmount
		}
	}
	whole, err := strconv.ParseUint(wholeText, 10, 64)
	if err != nil {
		return 0, ErrInvalidAmount
	}
	maxInt := uint64(^uint(0) >> 1)
	if whole > maxInt/100 {
		return 0, ErrInvalidAmount
	}
	cents := whole * 100
	if len(fractionText) > 0 {
		cents += uint64(fractionText[0]-'0') * 10
	}
	if len(fractionText) > 1 {
		cents += uint64(fractionText[1] - '0')
	}
	if len(fractionText) > 2 && fractionText[2] >= '5' {
		cents++
	}
	if cents > maxInt {
		return 0, ErrInvalidAmount
	}
	return int(cents), nil
}

// FormatCents returns a signed decimal amount with exactly two fraction digits.
func FormatCents(cents int) string {
	negative := cents < 0
	magnitude := uint64(cents)
	if negative {
		magnitude = ^magnitude + 1
	}
	whole, fraction := magnitude/100, magnitude%100
	result := strconv.FormatUint(whole, 10) + "."
	if fraction < 10 {
		result += "0"
	}
	result += strconv.FormatUint(fraction, 10)
	if negative {
		return "-" + result
	}
	return result
}
