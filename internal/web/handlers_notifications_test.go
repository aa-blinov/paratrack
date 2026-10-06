package web

import (
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

// signedInNotificationRequest builds the request context the authentication
// and workspace middleware install, then calls the handler directly: the two
// notification routes are registered by the transport wiring.
func (e *apiEnv) signedInNotificationRequest(path string, form url.Values) *httptest.ResponseRecorder {
	e.t.Helper()
	request := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.PostForm = form
	ctx := requestctx.WithActor(request.Context(), 1)
	ctx = requestctx.WithTeamID(ctx, 1)
	ctx = WithUser(ctx, appmodel.UserIdentity{ID: 1})
	ctx = WithTeam(ctx, model.Team{ID: 1, Name: "Workspace"})
	recorder := httptest.NewRecorder()
	switch path {
	case "/api/push/topics":
		e.srv.handleNotificationTopics(recorder, request.WithContext(ctx))
	default:
		e.srv.handleNotificationTest(recorder, request.WithContext(ctx))
	}
	return recorder
}

func TestNotificationTopicSaveKeepsTheWholeSelection(t *testing.T) {
	e := newAPIEnv(t)
	e.register("notify-topics@x.test")

	recorder := e.signedInNotificationRequest("/api/push/topics", url.Values{
		"topic": {appmodel.NotificationTopicPayrollPaid, appmodel.NotificationTopicGoalAchieved},
	})
	if recorder.Code != 200 {
		t.Fatalf("save = %d %s", recorder.Code, recorder.Body.String())
	}
	var stored notificationTopicsResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &stored); err != nil {
		t.Fatalf("save answered %d %s", recorder.Code, recorder.Body.String())
	}
	if len(stored.Muted) != 2 || stored.Muted[0] != appmodel.NotificationTopicPayrollPaid {
		t.Fatalf("stored selection = %v, want the two muted events", stored.Muted)
	}

	page := readBody(t, e.do("GET", "/settings/notifications", nil, nil))
	data := reactData[notifyPage](t, page)
	if len(data.Topics) != 3 {
		t.Fatalf("topics = %+v, want the three events the app sends", data.Topics)
	}
	muted := map[string]bool{}
	for _, topic := range data.Topics {
		muted[topic.Key] = topic.Muted
	}
	if !muted[appmodel.NotificationTopicPayrollPaid] || !muted[appmodel.NotificationTopicGoalAchieved] || muted[appmodel.NotificationTopicSessionStopped] {
		t.Fatalf("muted = %+v, want only the two chosen events off", muted)
	}

	// An empty form means "every event again", the state every account was in
	// before the setting existed.
	if recorder := e.signedInNotificationRequest("/api/push/topics", url.Values{}); recorder.Code != 200 {
		t.Fatalf("clear = %d %s", recorder.Code, recorder.Body.String())
	}
	page = readBody(t, e.do("GET", "/settings/notifications", nil, nil))
	for _, topic := range reactData[notifyPage](t, page).Topics {
		if topic.Muted {
			t.Fatalf("topic %s still muted after clearing the selection", topic.Key)
		}
	}
}

func TestNotificationTestNamesTheChannelOutcomeInsteadOfFailingQuietly(t *testing.T) {
	e := newAPIEnv(t)
	e.register("notify-test@x.test")

	// No subscribed device: the check must say the channel is not set up
	// instead of reporting a delivery that never happened.
	recorder := e.signedInNotificationRequest("/api/push/test", url.Values{})
	var answer notificationTestResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &answer); err != nil {
		t.Fatalf("check answered %d %s", recorder.Code, recorder.Body.String())
	}
	if recorder.Code != 200 || answer.Outcome != "no_channel" || answer.Delivered != 0 {
		t.Fatalf("check without a subscription = %d %+v, want no_channel with nothing delivered", recorder.Code, answer)
	}

	// An event the app never sends cannot be muted, so the stored selection
	// stays meaningful.
	recorder = e.signedInNotificationRequest("/api/push/topics", url.Values{"topic": {"invoice.paid"}})
	if recorder.Code != 400 {
		t.Fatalf("mute of an unsent event = %d %s", recorder.Code, recorder.Body.String())
	}
}
