package model

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

// Client is a party an invoice is addressed to. Before this entity existed the
// recipient was a set of columns on the project (client_name, client_details,
// client_email), which made two projects for the same customer two unrelated
// records and turned every invoice into a retyping exercise.
//
// The design goal is that a CRM can grow out of this without a rewrite:
// a client is workspace-scoped, uniquely named, archived instead of deleted,
// and carries its own currency and contacts. Projects and invoices point at
// it by ID; they also keep their own snapshot of the details so that renaming
// a customer never rewrites a document that was already sent.
type Client struct {
	ID     int64
	TeamID int64
	Name   string
	// Email is the invoice delivery address. Empty means the invoice is
	// sent by hand, so the UI must not offer a "send" action for it.
	Email string
	// Details is the postal or legal address printed on the invoice.
	Details string
	// Currency overrides the workspace default for documents addressed to
	// this client ('' = inherit from the workspace, then from the project).
	Currency string
	// Notes is internal: pricing history, how they prefer to pay, who
	// signs off. Never printed on an invoice.
	Notes     string
	Archived  bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ClientContact is one person at a client company. Kept as its own thin type
// rather than a free-form blob so that CRM-style "who do I email" lists have
// something to select from later.
type ClientContact struct {
	Name  string
	Email string
	Role  string
}

// ClientTagLimit and ClientTagMaxLength bound a client's tag set. A set this small
// stays readable on the card and in an agent's answer; a tag longer than a line
// is a sentence someone pasted, not a label.
const (
	ClientTagLimit     = 10
	ClientTagMaxLength = 40
)

// ErrInvalidClientTag is a tag that is empty, too long, contains a comma, or a set
// with more than ClientTagLimit distinct tags.
var ErrInvalidClientTag = errors.New("invalid client tag")

// NormalizeClientTags turns the tags a person typed into the stored form: trimmed,
// lower-cased, duplicates dropped, in the order they were given. The database
// calls it too, so no caller can store a tag the rules refuse.
func NormalizeClientTags(raw []string) ([]string, error) {
	out := make([]string, 0, len(raw))
	seen := make(map[string]bool, len(raw))
	for _, value := range raw {
		tag := strings.ToLower(strings.TrimSpace(value))
		if tag == "" || utf8.RuneCountInString(tag) > ClientTagMaxLength || strings.Contains(tag, ",") {
			return nil, ErrInvalidClientTag
		}
		if seen[tag] {
			continue
		}
		seen[tag] = true
		out = append(out, tag)
	}
	if len(out) > ClientTagLimit {
		return nil, ErrInvalidClientTag
	}
	return out, nil
}
