package money

import (
	"errors"
	"strconv"
	"testing"
)

func TestParseCents(t *testing.T) {
	for _, test := range []struct {
		input string
		want  int
	}{
		{input: "0", want: 0},
		{input: "12", want: 1200},
		{input: "12.3", want: 1230},
		{input: "12.34", want: 1234},
		{input: "12.344", want: 1234},
		{input: "12.345", want: 1235},
		{input: "+0.05", want: 5},
	} {
		t.Run(test.input, func(t *testing.T) {
			got, err := ParseCents(test.input)
			if err != nil {
				t.Fatalf("ParseCents(%q): %v", test.input, err)
			}
			if got != test.want {
				t.Fatalf("ParseCents(%q) = %d, want %d", test.input, got, test.want)
			}
		})
	}
}

func TestParseCentsRejectsInvalidAndOverflowValues(t *testing.T) {
	for _, input := range []string{"", ".5", "-1", "1e3", "1.2.3", "92233720368547759"} {
		t.Run(input, func(t *testing.T) {
			if _, err := ParseCents(input); !errors.Is(err, ErrInvalidAmount) {
				t.Fatalf("ParseCents(%q) error = %v, want ErrInvalidAmount", input, err)
			}
		})
	}
}

func TestFormatCents(t *testing.T) {
	minInt := -int(^uint(0)>>1) - 1
	minText := strconv.FormatInt(int64(minInt), 10)
	minWant := minText[:len(minText)-2] + "." + minText[len(minText)-2:]
	for _, test := range []struct {
		cents int
		want  string
	}{
		{cents: 0, want: "0.00"},
		{cents: 5, want: "0.05"},
		{cents: 12345, want: "123.45"},
		{cents: -12345, want: "-123.45"},
		{cents: minInt, want: minWant},
	} {
		if got := FormatCents(test.cents); got != test.want {
			t.Errorf("FormatCents(%d) = %q, want %q", test.cents, got, test.want)
		}
	}
}

func TestPriceCentsRejectsOverflow(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	for _, test := range []struct {
		name    string
		seconds int
		rate    int
	}{
		{name: "priced amount exceeds int", seconds: 3600 * 100, rate: maxInt},
		{name: "negative rate", seconds: 3600, rate: -1},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := PriceCents(test.seconds, test.rate); err == nil {
				t.Fatal("expected invalid or overflowing price to fail")
			}
		})
	}
}

func TestPriceCentsRoundsWithoutIntermediateOverflow(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	got, err := PriceCents(36, maxInt)
	if err != nil {
		t.Fatalf("price one hundredth hour at max rate: %v", err)
	}
	want := maxInt / 100
	if maxInt%100 >= 50 {
		want++
	}
	if got != want {
		t.Fatalf("PriceCents(36, maxInt) = %d, want %d", got, want)
	}
}

func TestAddIntRejectsOverflow(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	minInt := -maxInt - 1
	for _, test := range []struct {
		left, right int
	}{
		{left: maxInt, right: 1},
		{left: minInt, right: -1},
	} {
		if _, err := AddInt(test.left, test.right); !errors.Is(err, ErrOverflow) {
			t.Errorf("AddInt(%d, %d) error = %v, want ErrOverflow", test.left, test.right, err)
		}
	}
	if got, err := AddInt(20, 22); err != nil || got != 42 {
		t.Errorf("AddInt(20, 22) = %d, %v; want 42, nil", got, err)
	}
}

func TestPercentRatioHandlesScalingAndOverflow(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	for _, test := range []struct {
		name                    string
		part, total, scale, div int
		want                    int
	}{
		{name: "ordinary percentage", part: 7, total: 5, scale: 100, div: 1, want: 140},
		{name: "duration in seconds over minutes", part: 90, total: 1, scale: 5, div: 3, want: 150},
		{name: "large inputs", part: maxInt, total: maxInt, scale: 100, div: 1, want: 100},
		{name: "saturates result", part: maxInt, total: 1, scale: maxInt, div: 1, want: maxInt},
		{name: "non-positive denominator", part: 1, total: 0, scale: 100, div: 1, want: 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := PercentRatio(test.part, test.total, test.scale, test.div); got != test.want {
				t.Fatalf("PercentRatio(%d, %d, %d, %d) = %d, want %d", test.part, test.total, test.scale, test.div, got, test.want)
			}
		})
	}
}
