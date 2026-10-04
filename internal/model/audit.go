package model

import (
	"time"
)

// AuditRecord is the complete actor and workspace scope for one audit event.
type AuditRecord struct {
	TeamID int64
	UserID int64
	Action string
	Target string
	Meta   string
	IP     string
}

// AuditEntry is an immutable security or operations event.
type AuditEntry struct {
	ID        int64
	TeamID    int64
	UserID    int64
	Action    string
	Target    string
	Meta      string
	IP        string
	CreatedAt time.Time
}
