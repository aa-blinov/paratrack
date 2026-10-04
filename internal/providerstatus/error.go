// Package providerstatus defines the safe error shape returned for an
// unsuccessful external provider response.
package providerstatus

import "fmt"

// Error records the provider and HTTP status without retaining its response
// body, which may contain account data or implementation details.
type Error struct {
	Vendor     string
	StatusCode int
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s %d", e.Vendor, e.StatusCode)
}
