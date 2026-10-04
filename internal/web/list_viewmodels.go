package web

import "github.com/aa-blinov/paratrack/internal/i18n"

// Fragment wrappers give {{.T}} a language even when the item list is
// empty (a bare []tagWithCount has no method to call).

type tagsListVM struct {
	Lang      string
	Tags      []tagWithCount
	CanManage bool // shared tags: only managers delete them
}

func (tagsListVM) isTemplateData()         {}
func (tagsListVM) isFragmentTemplateData() {}

func (v tagsListVM) T(key string) string { return i18n.T(i18n.Lang(v.Lang), key) }

type goalsListVM struct {
	Lang      string
	Goals     []goalView
	CanManage bool // goals are the team's: managers set and remove them
}

func (goalsListVM) isTemplateData()         {}
func (goalsListVM) isFragmentTemplateData() {}

func (v goalsListVM) T(key string) string { return i18n.T(i18n.Lang(v.Lang), key) }

type activeListVM struct {
	Lang     string
	Items    []sessionView // active-list ranges over Items
	FirstRun bool          // nothing ever tracked: the empty state teaches the start
	Projects []projectView // the per-row project picker
	Running  int           // rows not paused, for "pause all"
}

func (activeListVM) isTemplateData()         {}
func (activeListVM) isFragmentTemplateData() {}

func (v activeListVM) T(key string) string { return i18n.T(i18n.Lang(v.Lang), key) }

func countRunning(views []sessionView) int {
	n := 0
	for _, view := range views {
		if !view.Paused {
			n++
		}
	}
	return n
}
