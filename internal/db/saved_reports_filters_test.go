package db

import (
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
)

// seedSavedReportProject gives a workspace a project and returns its id.
func seedSavedReportProject(t *testing.T, d *DB, teamID, callerID int64, name, slug string) int64 {
	t.Helper()
	project, err := d.CreateProjectWithBilling(t.Context(), appmodel.ProjectCreateRequest{
		TeamID: teamID, CallerID: callerID, Name: name, Slug: slug,
	})
	if err != nil {
		t.Fatalf("seed project: %v", err)
	}
	return project.ID
}

// seedSavedReportTag gives a workspace a tag and returns its id.
func seedSavedReportTag(t *testing.T, d *DB, teamID, callerID int64, name string) int64 {
	t.Helper()
	tag, err := d.CreateTagForMember(t.Context(), appmodel.TagCreateRequest{
		TeamID: teamID, CallerID: callerID, Name: name,
	})
	if err != nil {
		t.Fatalf("seed tag: %v", err)
	}
	return tag.ID
}

// savedReportFilter reads the preset's filters back through the public list
// query — the same path the /stats page and its links use.
func savedReportFilter(t *testing.T, d *DB, teamID int64, name string) (slug, tag string) {
	t.Helper()
	reports, err := d.ListSavedReports(t.Context(), teamID)
	if err != nil {
		t.Fatalf("list saved reports: %v", err)
	}
	for _, report := range reports {
		if report.Name == name {
			return report.ProjectSlug, report.Tag
		}
	}
	t.Fatalf("preset %q is missing from the team's list", name)
	return "", ""
}

func TestSavedReportTagFilterSurvivesTagRename(t *testing.T) {
	d := openTestDB(t)
	teamID := seedTeam(t, d, "Preset tag rename", "preset-tag-rename")
	ownerID := teamOwner(t, d, teamID)
	tagID := seedSavedReportTag(t, d, teamID, ownerID, "deep-wrok")

	report, err := d.CreateSavedReport(t.Context(), appmodel.SavedReportCreateRequest{
		TeamID: teamID, ActorID: ownerID, Name: "deep days", Period: "week", Tag: "deep-wrok",
	})
	if err != nil {
		t.Fatalf("save preset on the tag: %v", err)
	}
	if report.Tag != "deep-wrok" {
		t.Fatalf("saved preset filter = %q, want deep-wrok", report.Tag)
	}
	if _, err := d.RenameTagForManager(t.Context(), appmodel.TagRenameRequest{
		TeamID: teamID, CallerID: ownerID, TagID: tagID, Name: "deep-work",
	}); err != nil {
		t.Fatalf("rename tag: %v", err)
	}

	slug, tag := savedReportFilter(t, d, teamID, "deep days")
	if tag != "deep-work" {
		t.Fatalf("preset filter after rename = %q, want deep-work; a rename must not silently empty it", tag)
	}
	if slug != "" {
		t.Fatalf("preset project filter = %q, want none; the preset only filtered by tag", slug)
	}
}

func TestSavedReportProjectFilterSurvivesProjectRename(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "Preset project rename", "preset-project-rename")
	ownerID := teamOwner(t, d, teamID)
	projectID := seedSavedReportProject(t, d, teamID, ownerID, "EORA RAG", "eora-rag")

	if _, err := d.CreateSavedReport(ctx, appmodel.SavedReportCreateRequest{
		TeamID: teamID, ActorID: ownerID, Name: "rag week", Period: "week", ProjectSlug: "eora-rag",
	}); err != nil {
		t.Fatalf("save preset on the project: %v", err)
	}
	// The rename the interface offers today changes the project's name; its
	// slug — what the preset filter is written in — is untouched.
	if _, err := d.UpdateProjectWithOptions(ctx, appmodel.ProjectUpdateRequest{
		TeamID: teamID, ProjectID: projectID, CallerID: ownerID, Update: appmodel.ProjectUpdate{Name: "EORA Rag v2"},
	}); err != nil {
		t.Fatalf("rename project: %v", err)
	}

	slug, tag := savedReportFilter(t, d, teamID, "rag week")
	if slug != "eora-rag" {
		t.Fatalf("preset project filter after rename = %q, want eora-rag", slug)
	}
	if tag != "" {
		t.Fatalf("preset tag filter = %q, want none; the preset only filtered by project", tag)
	}
}

func TestSavedReportProjectFilterFollowsChangedProjectSlug(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "Preset project slug", "preset-project-slug")
	ownerID := teamOwner(t, d, teamID)
	projectID := seedSavedReportProject(t, d, teamID, ownerID, "EORA RAG", "eora-rag")

	if _, err := d.CreateSavedReport(ctx, appmodel.SavedReportCreateRequest{
		TeamID: teamID, ActorID: ownerID, Name: "rag week", Period: "week", ProjectSlug: "eora-rag",
	}); err != nil {
		t.Fatalf("save preset on the project: %v", err)
	}
	// Slugs are not editable through the interface yet, so the change is made
	// directly: the point of the test is that the filter follows the project
	// row rather than the string it was saved with.
	if _, err := d.TestSQL().ExecContext(ctx,
		`UPDATE projects SET slug = 'eora-rag-v2' WHERE id = ?`, projectID); err != nil {
		t.Fatalf("change project slug: %v", err)
	}

	slug, _ := savedReportFilter(t, d, teamID, "rag week")
	if slug != "eora-rag-v2" {
		t.Fatalf("preset project filter after slug change = %q, want eora-rag-v2", slug)
	}
}

func TestSavedReportTagFilterEmptiedWhenTagDeleted(t *testing.T) {
	d := openTestDB(t)
	teamID := seedTeam(t, d, "Preset tag deleted", "preset-tag-deleted")
	ownerID := teamOwner(t, d, teamID)
	tagID := seedSavedReportTag(t, d, teamID, ownerID, "deep-work")

	if _, err := d.CreateSavedReport(t.Context(), appmodel.SavedReportCreateRequest{
		TeamID: teamID, ActorID: ownerID, Name: "deep days", Period: "week", Tag: "deep-work",
	}); err != nil {
		t.Fatalf("save preset on the tag: %v", err)
	}
	if err := d.DeleteTagForManager(t.Context(), appmodel.TagDeleteRequest{
		TeamID: teamID, CallerID: ownerID, TagID: tagID,
	}); err != nil {
		t.Fatalf("delete tag: %v", err)
	}

	// A deleted tag must not leave the preset broken: it reads as "no tag
	// filter", never as the name that no longer exists.
	slug, tag := savedReportFilter(t, d, teamID, "deep days")
	if tag != "" {
		t.Fatalf("preset tag filter after delete = %q, want an empty filter", tag)
	}
	if slug != "" {
		t.Fatalf("preset project filter = %q, want none; the preset only filtered by tag", slug)
	}
}

func TestSavedReportProjectFilterEmptiedWhenProjectDeleted(t *testing.T) {
	d := openTestDB(t)
	teamID := seedTeam(t, d, "Preset project deleted", "preset-project-deleted")
	ownerID := teamOwner(t, d, teamID)
	projectID := seedSavedReportProject(t, d, teamID, ownerID, "EORA RAG", "eora-rag")

	if _, err := d.CreateSavedReport(t.Context(), appmodel.SavedReportCreateRequest{
		TeamID: teamID, ActorID: ownerID, Name: "rag week", Period: "week", ProjectSlug: "eora-rag",
	}); err != nil {
		t.Fatalf("save preset on the project: %v", err)
	}
	if err := d.DeleteProject(t.Context(), appmodel.ProjectMutationRequest{
		TeamID: teamID, ProjectID: projectID, CallerID: ownerID,
	}); err != nil {
		t.Fatalf("delete project: %v", err)
	}

	slug, _ := savedReportFilter(t, d, teamID, "rag week")
	if slug != "" {
		t.Fatalf("preset project filter after delete = %q, want an empty filter", slug)
	}
}

func TestSavedReportUnknownTagOrProjectSavesWithoutFilter(t *testing.T) {
	d := openTestDB(t)
	teamID := seedTeam(t, d, "Preset unknown filter", "preset-unknown-filter")
	ownerID := teamOwner(t, d, teamID)

	// The filter pickers only offer existing rows, but a preset can also be
	// saved from a stale link or an import; that must not fail the save.
	report, err := d.CreateSavedReport(t.Context(), appmodel.SavedReportCreateRequest{
		TeamID: teamID, ActorID: ownerID, Name: "ghost filters", Period: "week",
		ProjectSlug: "no-such-project", Tag: "no-such-tag",
	})
	if err != nil {
		t.Fatalf("save preset naming rows that don't exist: %v", err)
	}
	if report.ProjectSlug != "" || report.Tag != "" {
		t.Fatalf("preset filters = (%q, %q), want both empty", report.ProjectSlug, report.Tag)
	}

	slug, tag := savedReportFilter(t, d, teamID, "ghost filters")
	if slug != "" || tag != "" {
		t.Fatalf("stored preset filters = (%q, %q), want both empty", slug, tag)
	}
}

func TestSavedReportWithoutFiltersReadsBackUnchanged(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "Preset no filters", "preset-no-filters")
	ownerID := teamOwner(t, d, teamID)
	// Rows next to it in the same workspace, so the list read has something
	// to resolve and to leave alone.
	seedSavedReportProject(t, d, teamID, ownerID, "EORA RAG", "eora-rag")
	seedSavedReportTag(t, d, teamID, ownerID, "deep-work")

	created, err := d.CreateSavedReport(ctx, appmodel.SavedReportCreateRequest{
		TeamID: teamID, ActorID: ownerID, Name: "everything", Period: "month",
	})
	if err != nil {
		t.Fatalf("save preset without filters: %v", err)
	}
	if created.ProjectSlug != "" || created.Tag != "" {
		t.Fatalf("preset filters = (%q, %q), want both empty", created.ProjectSlug, created.Tag)
	}

	slug, tag := savedReportFilter(t, d, teamID, "everything")
	if slug != "" || tag != "" {
		t.Fatalf("stored preset filters = (%q, %q), want both empty", slug, tag)
	}
}

func TestSavedReportFilterIgnoresAnotherTeamsTagWithSameName(t *testing.T) {
	d := openTestDB(t)
	teamID := seedTeam(t, d, "Preset team one", "preset-team-one")
	ownerID := teamOwner(t, d, teamID)
	otherTeamID := seedTeam(t, d, "Preset team two", "preset-team-two")
	otherOwnerID := teamOwner(t, d, otherTeamID)
	seedSavedReportTag(t, d, otherTeamID, otherOwnerID, "deep-work")

	if _, err := d.CreateSavedReport(t.Context(), appmodel.SavedReportCreateRequest{
		TeamID: teamID, ActorID: ownerID, Name: "deep days", Period: "week", Tag: "deep-work",
	}); err != nil {
		t.Fatalf("save preset naming another team's tag: %v", err)
	}

	// The tag exists, but not in this workspace: adopting it would leak another
	// workspace's filter into this preset.
	if _, tag := savedReportFilter(t, d, teamID, "deep days"); tag != "" {
		t.Fatalf("preset tag filter = %q, want empty; the tag belongs to another workspace", tag)
	}
}

func TestMigrateSavedReportFilterIDsResolvesLegacyTextFilters(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "Preset legacy rows", "preset-legacy-rows")
	ownerID := teamOwner(t, d, teamID)
	seedSavedReportProject(t, d, teamID, ownerID, "EORA RAG", "eora-rag")
	seedSavedReportTag(t, d, teamID, ownerID, "deep-work")
	otherTeamID := seedTeam(t, d, "Preset legacy other", "preset-legacy-other")
	seedSavedReportProject(t, d, otherTeamID, teamOwner(t, d, otherTeamID), "Other", "other-project")

	// Rows written before presets stored ids: the filters are plain text.
	now := FormatTime(time.Now().UTC())
	for _, legacy := range []struct{ name, slug, tag string }{
		{"legacy matched", "eora-rag", "deep-work"},
		{"legacy unknown project", "gone-project", "deep-work"},
		{"legacy unknown tag", "eora-rag", "gone-tag"},
		{"legacy nothing", "", ""},
		{"legacy other team's project", "other-project", ""},
	} {
		if _, err := d.TestSQL().ExecContext(ctx,
			`INSERT INTO saved_reports (team_id, name, period, project_slug, tag, created_by, created_at)
			 VALUES (?, ?, 'week', ?, ?, ?, ?)`,
			teamID, legacy.name, legacy.slug, legacy.tag, ownerID, now); err != nil {
			t.Fatalf("seed legacy preset %q: %v", legacy.name, err)
		}
	}

	// Opening the database already ran the startup steps, including this
	// migration, so forget that before replaying it over the legacy rows.
	if _, err := d.TestSQL().ExecContext(ctx,
		`DELETE FROM paratrack_migrations WHERE name = '20261006_saved_report_filter_ids'`); err != nil {
		t.Fatalf("clear migration marker: %v", err)
	}
	if err := d.migrateSavedReportFilterIDs(ctx); err != nil {
		t.Fatalf("backfill preset filters: %v", err)
	}
	for name, want := range map[string][2]string{
		"legacy matched":              {"eora-rag", "deep-work"},
		"legacy unknown project":      {"", "deep-work"},
		"legacy unknown tag":          {"eora-rag", ""},
		"legacy nothing":              {"", ""},
		"legacy other team's project": {"", ""},
	} {
		slug, tag := savedReportFilter(t, d, teamID, name)
		if slug != want[0] || tag != want[1] {
			t.Fatalf("preset %q filters = (%q, %q), want (%q, %q)", name, slug, tag, want[0], want[1])
		}
	}

	// Running the backfill again must not change anything: rows that already
	// resolved are left alone instead of being matched a second time.
	if err := d.migrateSavedReportFilterIDs(ctx); err != nil {
		t.Fatalf("second backfill pass: %v", err)
	}
	if _, err := d.TestSQL().ExecContext(ctx, `DELETE FROM paratrack_migrations WHERE name = '20261006_saved_report_filter_ids'`); err != nil {
		t.Fatalf("clear backfill marker: %v", err)
	}
	if err := d.migrateSavedReportFilterIDs(ctx); err != nil {
		t.Fatalf("replay backfill after clearing its marker: %v", err)
	}
	if slug, tag := savedReportFilter(t, d, teamID, "legacy matched"); slug != "eora-rag" || tag != "deep-work" {
		t.Fatalf("preset filters after replay = (%q, %q), want (eora-rag, deep-work)", slug, tag)
	}
}
