// Package catalog is the integration marketplace + report-template
// registry. Both are code-defined (no DB rows) so a deploy can add
// providers without a migration.
package catalog

// Integration describes one marketplace entry.
type Integration struct {
	ID          string `json:"id"` // github, trello, …
	Name        string `json:"name"`
	Category    string `json:"category"` // dev | pm | personal
	Icon        string `json:"icon"`     // lucide name
	Blurb       string `json:"blurb"`
	SecretHint  string `json:"secret_hint"`
	TargetHint  string `json:"target_hint"`
	Available   bool   `json:"available"` // false → "coming soon"
}

// Integrations is the full marketplace list, grouped by category order.
func Integrations() []Integration {
	return []Integration{
		{ID: "github", Name: "GitHub", Category: "dev", Icon: "folder",
			Blurb: "Import open issues from a repository and start timers on them.",
			SecretHint: "Personal access token (repo scope)", TargetHint: "owner/repo", Available: true},
		{ID: "gitlab", Name: "GitLab", Category: "dev", Icon: "folder",
			Blurb: "Import open issues from a project, self-hosted or gitlab.com.",
			SecretHint: "PAT (glpat-…)", TargetHint: "group/project", Available: true},
		{ID: "jira", Name: "Jira", Category: "dev", Icon: "list-checks",
			Blurb: "Pull unresolved issues via JQL and track time against them.",
			SecretHint: "email:api-token or PAT", TargetHint: "PROJ or JQL", Available: true},
		{ID: "clickup", Name: "ClickUp", Category: "pm", Icon: "check",
			Blurb: "Import open tasks from a list.",
			SecretHint: "API token (pk_…)", TargetHint: "list id", Available: true},
		{ID: "asana", Name: "Asana", Category: "pm", Icon: "check",
			Blurb: "Import incomplete tasks from a project.",
			SecretHint: "Personal access token", TargetHint: "project GID", Available: true},
		{ID: "trello", Name: "Trello", Category: "pm", Icon: "folder",
			Blurb: "Import open cards from a board.",
			SecretHint: "key:token", TargetHint: "board id", Available: true},
		{ID: "notion", Name: "Notion", Category: "pm", Icon: "list-checks",
			Blurb: "Query a database and track time against its pages.",
			SecretHint: "Internal integration secret", TargetHint: "database id", Available: true},
		{ID: "todoist", Name: "Todoist", Category: "personal", Icon: "check",
			Blurb: "Import active tasks (optionally filtered to one project).",
			SecretHint: "API token", TargetHint: "project id (optional)", Available: true},
		{ID: "monday", Name: "monday.com", Category: "pm", Icon: "list-checks",
			Blurb: "Boards and items — coming soon.", Available: false},
		{ID: "basecamp", Name: "Basecamp", Category: "pm", Icon: "folder",
			Blurb: "To-dos and assignments — coming soon.", Available: false},
		{ID: "slack", Name: "Slack", Category: "dev", Icon: "zap",
			Blurb: "Start a timer from a /paratrack slash command — coming soon.", Available: false},
	}
}

// ReportTemplate is a named report preset: what it groups by and what
// columns it shows.
type ReportTemplate struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Blurb    string `json:"blurb"`
	Icon     string `json:"icon"`
	GroupBy  string `json:"group_by"` // project | activity | user | day
	Billable bool   `json:"billable"` // include rate/amount columns
}

// ReportTemplates lists the built-in report presets.
func ReportTemplates() []ReportTemplate {
	return []ReportTemplate{
		{ID: "by-project", Name: "Hours by project", Blurb: "Tracked time grouped per project.", Icon: "folder", GroupBy: "project", Billable: true},
		{ID: "by-activity", Name: "Hours by activity", Blurb: "Tracked time grouped per activity.", Icon: "activity", GroupBy: "activity", Billable: true},
		{ID: "by-day", Name: "Hours by day", Blurb: "Daily totals across the period.", Icon: "calendar", GroupBy: "day"},
		{ID: "billable", Name: "Billable summary", Blurb: "Billable hours × project rate → revenue.", Icon: "zap", GroupBy: "project", Billable: true},
		{ID: "utilization", Name: "Team utilization", Blurb: "Planned schedule vs actual tracked per person.", Icon: "users", GroupBy: "user"},
	}
}

// Report gets a template by id.
func Report(id string) (ReportTemplate, bool) {
	for _, t := range ReportTemplates() {
		if t.ID == id {
			return t, true
		}
	}
	return ReportTemplate{}, false
}

// IntegrationByID finds one marketplace entry.
func IntegrationByID(id string) (Integration, bool) {
	for _, it := range Integrations() {
		if it.ID == id {
			return it, true
		}
	}
	return Integration{}, false
}
