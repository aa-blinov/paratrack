package web

import "net/http"

// helpPage renders /help — the feature map. Static content only, but it
// still carries pageData so {{.T}} / nav Active work like every other page.
type helpPage struct {
	pageData
	// flash-banner partial reads these on every page that includes it.
	Flash   string
	FlashOK bool
}

func (s *Server) handleHelp(w http.ResponseWriter, r *http.Request) {
	lang := string(resolveLang(r))
	data := helpPage{
		pageData: pageData{
			Title:  "Help",
			Active: "help",
			Lang:   lang,
		},
	}
	s.renderPageForRequest(w, r, "Help", "help", "help", &data)
}
