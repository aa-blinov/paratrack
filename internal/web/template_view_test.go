package web

import (
	"bytes"
	"testing"
	"time"
)

// These representative renders check the field contract between templates
// and their presentation models. html/template accepts missing struct fields
// at parse time and reports them only when a particular page is rendered.
func TestInviteTemplatesRenderTheirViewModels(t *testing.T) {
	tmpl, err := parseTemplates(sentryState{})
	if err != nil {
		t.Fatalf("parse templates: %v", err)
	}

	t.Run("invite list", func(t *testing.T) {
		data := settingsPageData{
			Title: "Invites", Active: "settings-invites", Lang: "en",
			Team: teamView{ID: 1, Name: "Studio"}, User: userView{ID: 2, Email: "owner@example.test"},
			CanManage: true,
			Invites: []inviteView{{
				Token: "invite-token", CreatedAt: time.Unix(1, 0),
				ExpiresAt: time.Unix(2, 0), Live: true,
			}},
		}
		var output bytes.Buffer
		if err := tmpl.ExecuteTemplate(&output, "team-invites", data); err != nil {
			t.Fatalf("render team invites: %v", err)
		}
	})

	t.Run("invite acceptance", func(t *testing.T) {
		data := invitePage{
			Title: "Join team", Token: "invite-token", Lang: "en",
			Invite: inviteView{Expired: true}, Team: teamView{ID: 1, Name: "Studio"},
		}
		var output bytes.Buffer
		if err := tmpl.ExecuteTemplate(&output, "invite-accept", data); err != nil {
			t.Fatalf("render invite acceptance: %v", err)
		}
	})
}
