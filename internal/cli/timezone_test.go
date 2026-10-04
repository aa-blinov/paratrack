package cli

import (
	"bytes"
	"github.com/aa-blinov/paratrack/internal/appmodel"
	"strings"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/model"
)

func TestPrintActiveUsesRuntimeTimezone(t *testing.T) {
	location, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	started := time.Date(2026, 10, 3, 3, 0, 0, 0, time.UTC)
	runtime := &Runtime{
		Now: func() time.Time { return time.Date(2026, 10, 3, 12, 1, 0, 0, location) },
		Out: &output,
	}
	printActive(runtime, []appmodel.ActiveSession{{
		Session:  model.Session{StartAt: started, LastResumeAt: &started},
		Activity: model.Activity{Name: "work"},
	}})
	if !strings.Contains(output.String(), "12:00:00") || !strings.Contains(output.String(), "00:01:00") {
		t.Fatalf("active session output does not use Asia/Tokyo: %q", output.String())
	}
}
