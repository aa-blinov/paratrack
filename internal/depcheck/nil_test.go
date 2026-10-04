package depcheck

import "testing"

type dependency struct{}

func TestIsNilDetectsTypedNilInterface(t *testing.T) {
	var pointer *dependency
	var port any = pointer
	if !IsNil(port) {
		t.Fatal("IsNil(typed nil) = false, want true")
	}
	if IsNil(dependency{}) {
		t.Fatal("IsNil(value) = true, want false")
	}
}
