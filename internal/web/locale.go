package web

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/i18n"
)

// ---------------------------------------------------------------------------
// UI language
// ---------------------------------------------------------------------------

const langCookieName = "paratrack_lang"

// resolveLang picks the visitor's language: explicit cookie first, then
// Accept-Language, then English.
func resolveLang(r *http.Request) i18n.Lang {
	if c, err := r.Cookie(langCookieName); err == nil && c.Value != "" {
		return i18n.Normalize(c.Value)
	}
	al := r.Header.Get("Accept-Language")
	// First matching tag wins; "ru-RU,ru;q=0.9,en;q=0.8" → ru.
	// Match explicitly — i18n.Default is ru, so it cannot act as the
	// "not recognised" sentinel.
	for _, part := range strings.Split(al, ",") {
		tag := strings.ToLower(strings.TrimSpace(strings.SplitN(part, ";", 2)[0]))
		if tag == "" {
			continue
		}
		switch {
		case strings.HasPrefix(tag, "ru"):
			return i18n.Ru
		case strings.HasPrefix(tag, "en"):
			return i18n.En
		}
	}
	return i18n.Default
}

// rememberLanguage stores the language a browser session is using, so a letter
// the app sends this person is written in it (CRM-15). It writes only when the
// language changed, and returns the preferences the request should carry on.
// A failed save is logged and the request goes on with the old value.
func (s *Server) rememberLanguage(ctx context.Context, r *http.Request, userID, teamID int64, prefs Prefs) Prefs {
	lang := string(resolveLang(r))
	if prefs.Language == lang {
		return prefs
	}
	prefs.Language = lang
	if err := s.services.Preferences.Save(ctx, appmodel.PreferencesSaveRequest{
		UserID: userID, CallerID: userID, TeamID: teamID, Preferences: prefs,
	}); err != nil {
		s.logInternalError(fmt.Errorf("save language: %w", err))
	}
	return prefs
}

func setLangCookie(w http.ResponseWriter, r *http.Request, lang i18n.Lang) {
	http.SetCookie(w, &http.Cookie{
		Name:     langCookieName,
		Value:    string(lang),
		Path:     "/",
		SameSite: sameSiteFor(r),
		MaxAge:   365 * 24 * 3600,
		Secure:   isSecureRequest(r),
	})
}
