package web

import (
	"github.com/aa-blinov/paratrack/internal/i18n"
)

// tagChip is the lightweight view-model for a tag in the stats row
// and on the /tags management page. We don't need the timestamps here.
type tagChip struct {
	ID   int64
	Name string
}

// tagsData feeds tags.html.
type tagsData struct {
	pageData
	ReactTags   bool
	Tags        []tagWithCount
	AllTagNames []string // for autocomplete on the new-tag input
}

func (tagsData) usesReactApp() bool { return true }

// tagWithCount is a tag plus how many sessions carry it.
type tagWithCount struct {
	tagChip
	SessionCount int
	Lang         string
}

func (v tagWithCount) T(key string) string { return i18n.T(i18n.Lang(v.Lang), key) }
