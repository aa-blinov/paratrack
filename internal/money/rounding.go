// Package money contains deterministic currency arithmetic shared by
// application workflows, adapters, and persistence code.
package money

import (
	"errors"
	"math/big"
)

var ErrOverflow = errors.New("money amount overflow")

// HoursHundredths rounds tracked seconds to hundredths of an hour, half up.
func HoursHundredths(seconds int) int {
	if seconds <= 0 {
		return 0
	}
	return (seconds/3600)*100 + ((seconds%3600)*100+1800)/3600
}

// PriceCents prices tracked seconds from rounded hundredths of an hour and
// rounds the final amount half up to the nearest cent.
func PriceCents(seconds, rateCents int) (int, error) {
	if seconds <= 0 || rateCents == 0 {
		return 0, nil
	}
	if rateCents < 0 {
		return 0, ErrInvalidAmount
	}
	hundredths := uint64(HoursHundredths(seconds))
	rate := uint64(rateCents)
	whole, remainder := hundredths/100, hundredths%100
	maxInt := uint64(^uint(0) >> 1)
	if rate != 0 && whole > maxInt/rate {
		return 0, ErrOverflow
	}
	amount := whole * rate
	partial := remainder*(rate/100) + (remainder*(rate%100)+50)/100
	if partial > maxInt-amount {
		return 0, ErrOverflow
	}
	return int(amount + partial), nil
}

// AddCents adds monetary values while rejecting machine-int overflow.
func AddCents(left, right int) (int, error) {
	return AddInt(left, right)
}

// AddInt adds non-domain integer totals while rejecting machine-int overflow.
func AddInt(left, right int) (int, error) {
	maxInt := int(^uint(0) >> 1)
	minInt := -maxInt - 1
	if right > 0 && left > maxInt-right || right < 0 && left < minInt-right {
		return 0, ErrOverflow
	}
	return left + right, nil
}

// PercentRatio computes floor(part*numeratorScale/(total*denominatorScale))
// without intermediate integer overflow. Results larger than int can hold are
// saturated at max int; non-positive inputs produce zero.
func PercentRatio(part, total, numeratorScale, denominatorScale int) int {
	if part <= 0 || total <= 0 || numeratorScale <= 0 || denominatorScale <= 0 {
		return 0
	}
	numerator := new(big.Int).Mul(big.NewInt(int64(part)), big.NewInt(int64(numeratorScale)))
	denominator := new(big.Int).Mul(big.NewInt(int64(total)), big.NewInt(int64(denominatorScale)))
	result := numerator.Quo(numerator, denominator)
	maxInt := int(^uint(0) >> 1)
	if result.Cmp(big.NewInt(int64(maxInt))) > 0 {
		return maxInt
	}
	return int(result.Int64())
}
