// Package depcheck contains small checks used at dependency-injection
// boundaries.
package depcheck

import "reflect"

// IsNil reports whether value is nil, including a typed nil stored in an
// interface. It is intended for constructor validation of interface ports.
func IsNil(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}
