package appmodel

// AuditListQuery scopes an audit trail to one workspace and bounds the result.
type AuditListQuery struct {
	TeamID int64
	Limit  int
}
