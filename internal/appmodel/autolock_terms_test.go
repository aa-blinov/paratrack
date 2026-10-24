package appmodel

import "testing"

// Only the offered closing terms are valid. Zero is the off switch, and anything
// else, including a term the form does not list, is refused by the service.
func TestValidAutoLockDaysAcceptsOnlyTheOfferedTerms(t *testing.T) {
	for _, days := range []int{0, 3, 7, 14, 30} {
		if !ValidAutoLockDays(days) {
			t.Errorf("ValidAutoLockDays(%d) = false, want true", days)
		}
	}
	for _, days := range []int{-1, 1, 5, 8, 31, 365} {
		if ValidAutoLockDays(days) {
			t.Errorf("ValidAutoLockDays(%d) = true, want false", days)
		}
	}
}
