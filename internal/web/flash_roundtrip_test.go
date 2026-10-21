package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/aa-blinov/paratrack/internal/i18n"
)

// A flash reaches the page exactly as the server wrote it, whatever characters the
// message carries: the redirect address is its only way there.
func TestFlashSurvivesTheRedirectWhateverItContains(t *testing.T) {
	messages := []string{
		"За этот период нечего выставлять. В счёт попадают завершённые сессии оплачиваемых проектов со ставкой, которых ещё нет в других счетах; идущий таймер сначала остановите.",
		"Клиент Ромашка & Ко: счёт #12 создан, 50% оплачено + налог",
		"100% готово; проверьте %s и ?x=1",
		"Проект «A/B» не найден",
	}
	for _, msg := range messages {
		for _, ok := range []bool{true, false} {
			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodPost, "/invoices", nil)
			http.Redirect(w, r, "/invoices?flash="+encodeFlash(ok, msg), http.StatusSeeOther)

			location, err := url.Parse(w.Header().Get("Location"))
			if err != nil {
				t.Fatalf("location %q does not parse: %v", w.Header().Get("Location"), err)
			}
			got, gotOK := decodeFlash(location.Query().Get("flash"), i18n.Ru)
			if got != msg || gotOK != ok {
				t.Fatalf("flash %q (ok %v) came back as %q (ok %v)", msg, ok, got, gotOK)
			}
		}
	}
}
