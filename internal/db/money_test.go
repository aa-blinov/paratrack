package db

import "testing"

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
		if got := PriceCents(c.sec, c.rate); got != c.cents {
			t.Errorf("PriceCents(%d, %d) = %d, want %d", c.sec, c.rate, got, c.cents)
		}
	}
}
