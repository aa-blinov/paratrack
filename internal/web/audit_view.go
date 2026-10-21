package web

import (
	"strconv"
	"strings"

	"github.com/aa-blinov/paratrack/internal/i18n"
	"github.com/aa-blinov/paratrack/internal/model"
)

// auditRowTexts turns the codes a journal row keeps in its target and meta into
// the words a reader needs. Rows stay as they were written; the words are chosen
// at read time, so a row written before a label existed still reads the same way.
// The restore row lives in the trail of the workspace it restored, so its name is
// the workspace's current name.
func auditRowTexts(lang i18n.Lang, entry model.AuditEntry, names map[int64]string, teamID int64, teamName string) (target, detail string) {
	switch entry.Action {
	case "member.role":
		target = entry.Target
		if id, err := strconv.ParseInt(entry.Target, 10, 64); err == nil {
			if name, ok := names[id]; ok {
				target = name
			}
		}
		return target, roleLabel(lang, entry.Meta)
	case "auth.api_token_create", "auth.api_token_delete":
		// The token's name rides in meta. A revocation written before the name was
		// kept has none, and its row keeps the id it always had.
		if entry.Meta != "" {
			return entry.Meta, ""
		}
		return entry.Target, ""
	case "workspace.restore":
		id, err := strconv.ParseInt(strings.TrimPrefix(entry.Target, "team:"), 10, 64)
		if err != nil || id != teamID {
			return "", ""
		}
		return teamName, ""
	}
	// member.remove and invoice.delete already keep the name and the number as the
	// target; their meta holds a code and stays off the page.
	return entry.Target, ""
}

// roleLabel names a role for the journal. An unknown role code is shown as it is.
func roleLabel(lang i18n.Lang, role string) string {
	switch role {
	case string(model.TeamRoleOwner):
		return i18n.T(lang, "team.roleOwner")
	case string(model.TeamRoleAdmin):
		return i18n.T(lang, "team.roleAdmin")
	case string(model.TeamRoleMember):
		return i18n.T(lang, "team.roleMember")
	}
	return role
}

// auditRoleTargets lists the members a role change was made for. Such a member
// may have left since, so their name is looked up with the journal's own labels.
func auditRoleTargets(list []model.AuditEntry) []int64 {
	var ids []int64
	for _, entry := range list {
		if entry.Action != "member.role" {
			continue
		}
		if id, err := strconv.ParseInt(entry.Target, 10, 64); err == nil {
			ids = append(ids, id)
		}
	}
	return ids
}
