// Package cli holds terminal-prompt helpers used by interactive commands.
// They read from stdin and write to stderr so callers can keep stdout
// unpolluted (handy for piping/paratrack log > file).
package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// ErrCancelled is returned when the user types the cancel sentinel
// (a single dot on a line, matching the Python tracker convention).
var ErrCancelled = errors.New("cancelled")

// Prompt prints label, waits for a line on r, and returns it trimmed.
// An empty reply returns def; a single "." returns ErrCancelled.
func Prompt(r io.Reader, w io.Writer, label, def string) (string, error) {
	scanner := bufio.NewScanner(r)
	if def != "" {
		fmt.Fprintf(w, "%s [%s]: ", label, def)
	} else {
		fmt.Fprintf(w, "%s: ", label)
	}
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return "", err
		}
		return "", io.EOF
	}
	line := strings.TrimSpace(scanner.Text())
	if line == "." {
		return "", ErrCancelled
	}
	if line == "" {
		return def, nil
	}
	return line, nil
}

// Confirm asks y/n; y/Y/yes returns true, anything else false (default defaultYes).
func Confirm(r io.Reader, w io.Writer, label string, defaultYes bool) (bool, error) {
	hint := "y/N"
	if defaultYes {
		hint = "Y/n"
	}
	fmt.Fprintf(w, "%s [%s]: ", label, hint)
	scanner := bufio.NewScanner(r)
	if !scanner.Scan() {
		return defaultYes, nil
	}
	switch strings.ToLower(strings.TrimSpace(scanner.Text())) {
	case "y", "yes":
		return true, nil
	case "n", "no":
		return false, nil
	}
	return defaultYes, nil
}

// Choose prints label and a numbered menu; returns the index picked.
func Choose(r io.Reader, w io.Writer, label string, options []string, def int) (int, error) {
	fmt.Fprintf(w, "%s\n", label)
	for i, opt := range options {
		marker := "  "
		if i == def {
			marker = "> "
		}
		fmt.Fprintf(w, "  %s%d) %s\n", marker, i+1, opt)
	}
	fmt.Fprintf(w, "choose [1-%d, default %d] (. to cancel): ", len(options), def+1)
	scanner := bufio.NewScanner(r)
	if !scanner.Scan() {
		return def, nil
	}
	line := strings.TrimSpace(scanner.Text())
	if line == "." {
		return -1, ErrCancelled
	}
	if line == "" {
		return def, nil
	}
	var n int
	if _, err := fmt.Sscanf(line, "%d", &n); err != nil || n < 1 || n > len(options) {
		return -1, fmt.Errorf("invalid choice %q", line)
	}
	return n - 1, nil
}

// Stdin is the default reader; overridden in tests.
var Stdin io.Reader = os.Stdin

// Stderr is the default writer for prompts (so stdout stays clean).
var Stderr io.Writer = os.Stderr
