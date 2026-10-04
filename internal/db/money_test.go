package db

import (
	"math"
	"testing"
)

// Documents price the rounded hours, so hours × rate = amount always.
func TestPriceCents(t *testing.T) {
	cases := []struct {
		sec, rate, hundredths, cents int
	}{
		{3*3600 + 30*60 + 16, 5000, 350, 17500}, // 3.5044 h shows 3.50, bills 175.00 (was 175.22)
		{3*3600 + 30*60, 10000, 350, 35000},
		{17, 10000, 0, 0},       // under half a hundredth rounds to nothing
		{18, 10000, 1, 100},     // 0.005 h rounds half up to 0.01 h
		{5400, 4550, 150, 6825}, // 1.50 h × 45.50
		{1234, 3333, 34, 1133},  // 0.34 h × 33.33 = 11.3322 → 11.33
		{0, 5000, 0, 0},
	}
	for _, c := range cases {
		if h := HoursHundredths(c.sec); h != c.hundredths {
			t.Errorf("HoursHundredths(%d) = %d, want %d", c.sec, h, c.hundredths)
		}
		got, err := PriceCents(c.sec, c.rate)
		if err != nil {
			t.Fatalf("PriceCents(%d, %d): %v", c.sec, c.rate, err)
		}
		if got != c.cents {
			t.Errorf("PriceCents(%d, %d) = %d, want %d", c.sec, c.rate, got, c.cents)
		}
	}
}

func TestRoundBilled(t *testing.T) {
	up := BillingRules{RoundMinutes: 15, RoundMode: "up"}
	near := BillingRules{RoundMinutes: 15, RoundMode: "nearest"}
	for _, c := range []struct {
		sec  int
		r    BillingRules
		want int
	}{
		{61 * 60, up, 75 * 60}, {61 * 60, near, 60 * 60}, {68 * 60, near, 75 * 60},
		{10, BillingRules{}, 10}, {0, up, 0},
		{math.MaxInt, BillingRules{RoundMinutes: 60}, math.MaxInt},
		{math.MaxInt, BillingRules{RoundMinutes: 60, RoundMode: "up"}, math.MaxInt},
		{10, BillingRules{RoundMinutes: math.MaxInt}, 10},
	} {
		if got := RoundBilled(c.sec, c.r); got != c.want {
			t.Errorf("RoundBilled(%d, %+v) = %d, want %d", c.sec, c.r, got, c.want)
		}
	}
}
