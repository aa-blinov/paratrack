package web

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/aa-blinov/paratrack/internal/model"
)

type failingSessionTagReader struct{}

func (failingSessionTagReader) TagsForSessions(context.Context, int64, []int64) (map[int64][]model.Tag, error) {
	return nil, errors.New("tag store unavailable")
}

type failingProjectSummaryReader struct{}

func (failingProjectSummaryReader) Summaries(context.Context, int64, []int64) (map[int64]model.ProjectSummary, error) {
	return nil, errors.New("project store unavailable")
}

type recordingViewLogger struct{ messages []string }

func (l *recordingViewLogger) Printf(format string, args ...any) {
	l.messages = append(l.messages, fmt.Sprintf(format, args...))
}

func TestSessionEnrichmentFailuresAreLoggedAndNonFatal(t *testing.T) {
	rows := []sessionView{{ID: 11, ProjectID: 13}}
	logger := &recordingViewLogger{}

	hydrateSessionTags(context.Background(), failingSessionTagReader{}, 7, rows, logger)
	hydrateSessionProjects(context.Background(), failingProjectSummaryReader{}, 7, rows, logger)

	if len(logger.messages) != 2 {
		t.Fatalf("logged %d enrichment failures, want 2", len(logger.messages))
	}
	if !strings.Contains(logger.messages[0], "tag store unavailable") ||
		!strings.Contains(logger.messages[1], "project store unavailable") {
		t.Fatalf("enrichment log messages do not retain their causes: %v", logger.messages)
	}
	if len(rows[0].Tags) != 0 || rows[0].ProjectName != "" {
		t.Fatalf("failed enrichments changed the base session view: %+v", rows[0])
	}
}
