package web

import (
	"testing"
	"time"
)

// Every zone the menu offers must load, or saving it would fail (ADM-19).
func TestEveryTimezoneInTheMenuLoads(t *testing.T) {
	for _, name := range zones {
		if _, err := time.LoadLocation(name); err != nil {
			t.Errorf("zone %s does not load: %v", name, err)
		}
	}
	for _, want := range []string{"Europe/Paris", "Europe/Kyiv", "Asia/Shanghai", "America/Chicago"} {
		if !has(zones, want) {
			t.Errorf("the menu lacks %s", want)
		}
	}
}
