package push

import (
	"testing"

	"github.com/aa-blinov/paratrack/internal/appmodel"
)

// The email channel must reach people who never subscribed a browser (TASK-05,
// TIME-14). Only a muted topic keeps a letter away, and the device list is not
// consulted at all.
func TestMailRecipientsNeedNoSubscribedDevice(t *testing.T) {
	store := &topicStoreStub{muted: map[int64][]string{5: {appmodel.NotificationTopicTaskDue}}}
	service := topicService(t, store, &pushSenderStub{})

	got, err := service.MailRecipients(t.Context(), 7, []int64{4, 5}, appmodel.NotificationTopicTaskDue)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != 4 {
		t.Fatalf("mail recipients = %v, want [4]: the person without a device gets the letter, the one who muted the topic does not", got)
	}
	if len(store.targetsQueries) != 0 {
		t.Fatalf("the email channel read the device list: %+v", store.targetsQueries)
	}
}
