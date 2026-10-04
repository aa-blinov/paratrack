package catalog

import "testing"

func TestIntegrationRegistryHasUniqueResolvableIDs(t *testing.T) {
	seen := make(map[string]struct{})
	for _, item := range Integrations() {
		if item.ID == "" {
			t.Fatal("integration registry contains an empty ID")
		}
		if _, exists := seen[item.ID]; exists {
			t.Fatalf("integration registry contains duplicate ID %q", item.ID)
		}
		seen[item.ID] = struct{}{}

		got, ok := IntegrationByID(item.ID)
		if !ok || got != item {
			t.Fatalf("IntegrationByID(%q) = (%#v, %v), want (%#v, true)", item.ID, got, ok, item)
		}
	}
	if _, ok := IntegrationByID("missing"); ok {
		t.Fatal("IntegrationByID accepted an unknown ID")
	}
}

func TestReportTemplateRegistryHasUniqueResolvableIDs(t *testing.T) {
	seen := make(map[string]struct{})
	for _, item := range ReportTemplates() {
		if item.ID == "" {
			t.Fatal("report template registry contains an empty ID")
		}
		if _, exists := seen[item.ID]; exists {
			t.Fatalf("report template registry contains duplicate ID %q", item.ID)
		}
		seen[item.ID] = struct{}{}

		got, ok := Report(item.ID)
		if !ok || got != item {
			t.Fatalf("Report(%q) = (%#v, %v), want (%#v, true)", item.ID, got, ok, item)
		}
	}
	if _, ok := Report("missing"); ok {
		t.Fatal("Report accepted an unknown ID")
	}
}
