package web

import (
	"net/http"

	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/postcommit"
)

// audit records transport-level actions with the authenticated workspace,
// actor, and trusted client address. Audit storage failures are best effort,
// but remain visible in the configured server log.
func (s *Server) audit(r *http.Request, action, target, meta string) {
	uid := int64(0)
	if user, ok := UserFrom(r.Context()); ok {
		uid = user.ID
	}
	effectCtx, cancel := postcommit.NewContext(r.Context())
	defer cancel()
	if err := s.services.AuditLog.Record(effectCtx, model.AuditRecord{
		TeamID: teamID(r), UserID: uid, Action: action, Target: target, Meta: meta, IP: clientIP(r),
	}); err != nil {
		s.logger.Printf("web: record audit action %s: %v", action, err)
	}
}
