package appmodel

import "strings"

var sectionOrder = []string{"tasks", "graph", "goals", "tags", "invoices", "reports", "payroll", "schedule", "crm", "calendar", "integrations", "import"}

func IsKnownSection(key string) bool {
	for _, known := range sectionOrder {
		if key == known {
			return true
		}
	}
	return false
}

// EncodeSections returns the canonical persisted form. Empty storage remains
// the legacy all-enabled default; "none" represents an explicit all-disabled
// selection.
func EncodeSections(selected map[string]bool) string {
	keys := make([]string, 0, len(sectionOrder))
	for _, key := range sectionOrder {
		if selected[key] {
			keys = append(keys, key)
		}
	}
	if len(keys) == 0 {
		return "none"
	}
	return strings.Join(keys, ",")
}

// SectionChanges is the audit line for a section save: "+key" for each section
// switched on, then "-key" for each one switched off, in catalogue order. It is
// empty when the set did not change.
func SectionChanges(before, after map[string]bool) string {
	var on, off []string
	for _, key := range sectionOrder {
		switch {
		case after[key] && !before[key]:
			on = append(on, "+"+key)
		case before[key] && !after[key]:
			off = append(off, "-"+key)
		}
	}
	return strings.Join(append(on, off...), ", ")
}

func DecodeSections(stored string) map[string]bool {
	enabled := make(map[string]bool, len(sectionOrder))
	if strings.TrimSpace(stored) == "" {
		for _, key := range sectionOrder {
			enabled[key] = true
		}
		return enabled
	}
	for _, raw := range strings.Split(stored, ",") {
		key := strings.TrimSpace(raw)
		if IsKnownSection(key) {
			enabled[key] = true
		}
	}
	return enabled
}
