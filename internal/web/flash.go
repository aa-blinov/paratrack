package web

func encodeFlash(ok bool, msg string) string {
	if ok {
		return msg
	}
	return "e:" + msg
}

// handleAPITeamBilling saves rounding and the invoice number prefix.
