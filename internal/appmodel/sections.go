package appmodel

import "strings"

var sectionOrder = []string{"graph", "goals", "tags", "invoices", "reports", "payroll", "schedule", "integrations", "import"}

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
