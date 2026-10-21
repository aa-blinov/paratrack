package web

import "net/url"

// flashCode is the flash as the page reads it, before it goes into an address.
// Only a caller that hands the value to url.Values may use it: Encode escapes it there.
func flashCode(ok bool, msg string) string {
	if ok {
		return msg
	}
	return "e:" + msg
}

// encodeFlash is the flash as it goes into a redirect address. A raw ";" in a query
// makes net/url drop the whole parameter, and "&", "#", "+", "%" cut or change the
// value, so the message is escaped here, once, and every caller routes through it.
func encodeFlash(ok bool, msg string) string {
	return url.QueryEscape(flashCode(ok, msg))
}

// handleAPITeamBilling saves rounding and the invoice number prefix.
