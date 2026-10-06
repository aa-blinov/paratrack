package web

import (
	"errors"
	"net/http"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/i18n"
	"github.com/aa-blinov/paratrack/internal/model"
)

// ---------------------------------------------------------------------------
// What this account hears about, and a way to check the channel.
// ---------------------------------------------------------------------------

// notificationTopic is one selectable event. Label is a dictionary key: the
// React shell translates it in the reader's language, the transport never
// assembles page copy.
type notificationTopic struct {
	Key   string
	Label string
	Muted bool
}

// notificationTopicsResponse echoes the stored selection so the screen can
// trust what the server kept instead of what the browser hoped it sent.
type notificationTopicsResponse struct {
	Muted []string `json:"muted"`
}

func (notificationTopicsResponse) isJSONResponse() {}

// notificationTestResponse names what the push channel did. Outcome is the
// contract the screen renders: "delivered" means a device took it,
// "no_channel" means the account has no subscribed device the server can reach,
// "failed" means the push service refused it.
type notificationTestResponse struct {
	Outcome   string `json:"outcome"`
	Delivered int    `json:"delivered"`
}

func (notificationTestResponse) isJSONResponse() {}

// selectableNotificationTopics lists, in reading order, only the events the
// application really sends. Their keys must match the appmodel topics the
// producers name, so the checkboxes cannot offer something nobody delivers.
var selectableNotificationTopics = []struct {
	Key   string
	Label string
}{
	{appmodel.NotificationTopicSessionStopped, "push.ev1"},
	{appmodel.NotificationTopicGoalAchieved, "push.ev4"},
	{appmodel.NotificationTopicPayrollPaid, "push.ev3"},
}

// notificationTopicViews applies the caller's selection to the selectable
// topics. A person who never touched the setting has muted nothing and sees
// every event on.
func notificationTopicViews(muted []string) []notificationTopic {
	views := make([]notificationTopic, 0, len(selectableNotificationTopics))
	for _, topic := range selectableNotificationTopics {
		views = append(views, notificationTopic{
			Key:   topic.Key,
			Label: topic.Label,
			Muted: has(muted, topic.Key),
		})
	}
	return views
}

// handleNotificationTopics stores which events this account wants. The whole
// selection travels with every save, so clearing the last checkbox restores
// every event instead of leaving a stale partial answer behind.
func (s *Server) handleNotificationTopics(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.writeJSONStatus(w, http.StatusBadRequest, apiErrorResponse{Error: "invalid form"})
		return
	}
	user, ok := UserFrom(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	command := appmodel.NotificationTopicsCommand{
		TeamID: teamID(r), UserID: user.ID, CallerID: user.ID, Topics: r.PostForm["topic"],
	}
	if err := s.services.Push.SaveMutedTopics(operationContext(r), command); err != nil {
		switch {
		case errors.Is(err, appmodel.ErrInvalidNotificationTopic):
			s.writeJSONStatus(w, http.StatusBadRequest, apiErrorResponse{Error: "unknown notification topic"})
		case errors.Is(err, model.ErrForbidden):
			s.writeJSONStatus(w, http.StatusForbidden, apiErrorResponse{Error: "forbidden"})
		case errors.Is(err, model.ErrNotFound):
			s.writeJSONStatus(w, http.StatusNotFound, apiErrorResponse{Error: "not found"})
		default:
			s.writeInternalJSONError(w, err)
		}
		return
	}
	muted, err := s.services.Push.MutedTopics(r.Context(), appmodel.NotificationTopicsQuery{TeamID: teamID(r), UserID: user.ID})
	if err != nil {
		s.writeInternalJSONError(w, err)
		return
	}
	s.writeJSON(w, notificationTopicsResponse{Muted: muted})
}

// handleNotificationTest delivers one verification notification to the
// caller's own devices and reports the outcome. It is synchronous on purpose:
// a check that only enqueues work would report success for a channel that is
// not set up, which is the failure this screen exists to rule out.
func (s *Server) handleNotificationTest(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.writeJSONStatus(w, http.StatusBadRequest, apiErrorResponse{Error: "invalid form"})
		return
	}
	user, ok := UserFrom(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	lang := resolveLang(r)
	text := func(key string) string { return i18n.T(lang, key) }
	result, err := s.services.Push.TestNotification(r.Context(), appmodel.NotificationTestRequest{
		TeamID: teamID(r), UserID: user.ID, CallerID: user.ID,
		Title: text("push.testTitle"), Body: text("push.testBody"), URL: "/settings/notifications",
	})
	switch {
	case errors.Is(err, model.ErrForbidden):
		s.writeJSONStatus(w, http.StatusForbidden, apiErrorResponse{Error: "forbidden"})
		return
	case errors.Is(err, model.ErrNotFound):
		s.writeJSONStatus(w, http.StatusNotFound, apiErrorResponse{Error: "not found"})
		return
	case err != nil && !errors.Is(err, appmodel.ErrNotificationNotDelivered):
		// A failure to read the account's settings is an application error; a
		// refused delivery is the channel's answer and is reported below.
		s.writeInternalJSONError(w, err)
		return
	}
	s.writeJSON(w, notificationTestResponse{Outcome: testOutcome(result, err), Delivered: result.Delivered})
}

// testOutcome turns a delivery attempt into the word the screen shows. A
// completed attempt that reached no device means the account has no working
// subscription: the channel is not set up, and saying "sent" would be a lie.
func testOutcome(result appmodel.NotificationTestResult, err error) string {
	if err != nil {
		return "failed"
	}
	if result.Delivered > 0 {
		return "delivered"
	}
	return "no_channel"
}
