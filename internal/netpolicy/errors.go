// Package netpolicy contains errors shared by outbound network adapters and
// the application workflows that interpret their results.
package netpolicy

import "errors"

// ErrPrivateTarget reports an outbound URL that resolves to a non-public IP.
var ErrPrivateTarget = errors.New("outbound target is not a public address")
