package web

import "net/http"

// helpPage renders /help — the feature map. Static content only, but it
// still carries pageData so {{.T}} / nav Active work like every other page.
type helpPage struct {
	pageData
	HelpReact bool
	// flash-banner partial reads these on every page that includes it.
	Flash   string
	FlashOK bool
	// The "where is this configured" card links to real screens. A link the
	// reader cannot follow is worse than no link, so the page knows the role
	// and leaves out destinations behind a manager-only guard.
	CanManage bool
}

func (s *Server) handleHelp(w http.ResponseWriter, r *http.Request) {
	lang := string(resolveLang(r))
	data := helpPage{
		pageData: pageData{
			Title:    "Help",
			Active:   "help",
			Lang:     lang,
			ReactApp: true,
		},
		HelpReact: true,
		CanManage: canManage(r),
	}
	s.renderPageForRequest(w, r, "Help", "help", "help", &data)
}
