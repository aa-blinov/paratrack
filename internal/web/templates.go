package web

import (
	"fmt"
	"html/template"
	"io/fs"
	"net/url"
	"strings"

	"github.com/aa-blinov/paratrack/internal/i18n"
)

// parseTemplates parses every *.html under templates/. Files define
// blocks ("base", "content") so child pages compose into the shared
// shell automatically.
func parseTemplates(sentry sentryState) (*template.Template, error) {
	root := template.New("").Funcs(templateFuncs(sentry))
	matches, err := fs.Glob(assets, "templates/*.html")
	if err != nil {
		return nil, err
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("no templates found")
	}
	for _, name := range matches {
		b, err := assets.ReadFile(name)
		if err != nil {
			return nil, err
		}
		if _, err := root.New(strings.TrimPrefix(name, "templates/")).Parse(string(b)); err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
	}
	return root, nil
}

func templateFuncs(sentry sentryState) template.FuncMap {
	return template.FuncMap{
		// fmtDuration is the shared smart formatter from dto.go — keep the
		// template name in lock-step so cards and tables always agree.
		"fmtDuration":  fmtDuration,
		"asset":        func(p string) string { return "/static/" + p + "?v=" + assetVersion },
		"appImportMap": appImportMap,
		// tOr translates key, or returns fallback when the key is missing.
		"tOr": func(lang, key, fallback string) string {
			if t := i18n.T(i18n.Lang(lang), key); t != key {
				return t
			}
			return fallback
		},
		"splitComma": func(s string) []string { return strings.Split(s, ",") },
		// pathesc is for a path segment: urlquery turns spaces into "+", which
		// a path keeps literally ("Только эта" 404'd on names with spaces).
		"pathesc":      url.PathEscape,
		"fmtDurL":      func(lang string, sec int) string { return fmtDurL(i18n.Lang(lang), sec) },
		"inkFor":       inkFor,
		"activityMark": activityMark,
		"icon":         iconHTML,
		"sentryDSN":    func() string { return sentry.publicDSN() },
		"sentryEnv":    func() string { return sentry.environment },
		"release":      func() string { return "paratrack@" + assetVersion },
	}
}

// iconHTML renders a Lucide glyph from the vendored sprite:
//
//	{{icon "play"}}              → <svg class="icon"><use href="…#i-play"/></svg>
//	{{icon "play" "icon-lg"}}
//
// Names are Lucide kebab-case (play, pause, square, target, tag, …).
// The sprite lives at /static/icons.svg; each <symbol> is prefixed i-.
func iconHTML(name string, classes ...string) template.HTML {
	if len(classes) > 1 {
		return ""
	}
	cls := "icon"
	if len(classes) > 0 && classes[0] != "" {
		cls = classes[0]
	}
	for _, r := range cls {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '-' || r == '_' || r == ' ') {
			return ""
		}
	}
	// Name is constrained to the Lucide kebab set so it can't inject.
	for _, r := range name {
		if !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') && r != '-' {
			return ""
		}
	}
	return template.HTML(`<svg class="` + cls + `" aria-hidden="true"><use href="/static/icons.svg#i-` + name + `"></use></svg>`)
}
