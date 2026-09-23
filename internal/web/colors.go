package web

// colorFor used to hash activity names into a 12-colour palette. The
// rainbow was dropped because it read as decorative noise; activity
// identity is now carried by the name alone. Kept as a no-op returning
// a neutral grey so existing call sites that still emit a Color field
// (for HTML / JSON) compile.
func colorFor(name string) string {
	return "#6b7280"
}
