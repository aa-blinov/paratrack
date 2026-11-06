package main

import (
	"path/filepath"
)

// globSlash lists files like filepath.Glob but with forward slashes. The
// exception lists and the package paths are written with slashes, so a
// Windows separator would silently miss them.
func globSlash(pattern string) ([]string, error) {
	matches, err := filepath.Glob(pattern)
	for i, match := range matches {
		matches[i] = filepath.ToSlash(match)
	}
	return matches, err
}
