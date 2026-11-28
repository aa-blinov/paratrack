package app

import "testing"

// Crossing a limit is a warning, never a congratulation.
func TestGoalPushTitle(t *testing.T) {
	if goalPushTitle("max") != "Лимит превышен" || goalPushTitle("min") != "Цель достигнута" || goalPushTitle("") != "Цель достигнута" {
		t.Fatal("goal push titles")
	}
}
