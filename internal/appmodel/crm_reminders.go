package appmodel

// CRM item kinds a next-step reminder can be about.
const (
	CRMItemDeal = "deal"
	CRMItemLead = "lead"
)

// CRM reminder channels. A claim per channel lets the push and the letter for
// one item go out on their own schedule within the same day.
const (
	CRMChannelPush  = "push"
	CRMChannelEmail = "email"
)

// CRMStep is a deal or lead whose next step is on or before a day. Zone is the
// workspace time zone the reminder pass reads "today" in; "" means UTC.
type CRMStep struct {
	TeamID     int64
	Kind       string
	ItemID     int64
	Name       string
	OwnerID    int64
	NextStepAt string
	Zone       string
}

// CRMReminderClaimRequest records that one item's reminder went out on one channel for
// one workspace day. Day is YYYY-MM-DD in that workspace's zone.
type CRMReminderClaimRequest struct {
	TeamID  int64
	Kind    string
	ItemID  int64
	Day     string
	Channel string
}
