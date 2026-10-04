// Package teamslug contains the shared, deterministic slug rules for teams.
package teamslug

import (
	"fmt"
	"strings"

	"github.com/aa-blinov/paratrack/internal/translit"
)

// Slugify creates a URL-safe team slug from a user-facing name.
func Slugify(name string) string {
	var builder strings.Builder
	builder.Grow(len(name))
	for _, char := range translit.Latin(strings.TrimSpace(name)) {
		switch {
		case char >= 'a' && char <= 'z', char >= '0' && char <= '9':
			builder.WriteRune(char)
		case char == ' ' || char == '-' || char == '_':
			builder.WriteRune('-')
		}
	}
	slug := strings.Trim(builder.String(), "-")
	if slug == "" {
		return "team"
	}
	return slug
}

// PersonalSlug derives a stable slug for the personal team created with an account.
func PersonalSlug(userID int64, name string) string {
	return fmt.Sprintf("personal-%d-%s", userID, Slugify(name))
}
