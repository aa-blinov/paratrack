package reminders

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/i18n"
)

// CRMAudience decides who hears about a next step and how: the topic switch,
// the language the person last used, and the mailbox the letter goes to.
type CRMAudience interface {
	// Wanting keeps the people who have not switched the topic off.
	Wanting(ctx context.Context, teamID int64, userIDs []int64, topic string) ([]int64, error)
	// Language is the language the person last used, "" when it is not known.
	Language(ctx context.Context, userID int64) string
	// QueueLetter freezes a letter for the person's mailbox. A person without an
	// address gets no letter, which is not an error.
	QueueLetter(ctx context.Context, teamID, userID int64, subject, text string) error
}

// crmSteps reminds each owner about the deals and leads whose next step falls on
// today in their workspace's zone. Each item goes out once per channel per day:
// the claim is stored before the push or the letter is handed over, so a repeat
// pass the same day sends nothing. A person who switched the topic off is skipped
// before any claim, so the reminder is not spent on them.
func (s *Service) crmSteps(ctx context.Context, now time.Time) error {
	if s.audience == nil {
		return nil
	}
	steps, err := s.store.CRMStepsDue(ctx, now.AddDate(0, 0, 1).Format("2006-01-02"))
	if err != nil {
		return err
	}
	type owner struct{ teamID, userID int64 }
	var owners []owner
	byOwner := map[owner][]appmodel.CRMStep{}
	for _, step := range steps {
		key := owner{step.TeamID, step.OwnerID}
		if _, seen := byOwner[key]; !seen {
			owners = append(owners, key)
		}
		byOwner[key] = append(byOwner[key], step)
	}
	var mailErrs []error
	for _, key := range owners {
		wanted, err := s.audience.Wanting(ctx, key.teamID, []int64{key.userID}, TopicCRMNextStep)
		if err != nil {
			return err
		}
		if len(wanted) == 0 {
			continue
		}
		loc, err := workspaceLocation(byOwner[key][0].Zone)
		if err != nil {
			return err
		}
		day := now.In(loc).Format("2006-01-02")
		lang := i18n.Normalize(s.audience.Language(ctx, key.userID))
		var mailed []appmodel.CRMStep
		for _, step := range byOwner[key] {
			if step.NextStepAt > day {
				continue
			}
			claim := appmodel.CRMReminderClaimRequest{TeamID: key.teamID, Kind: step.Kind, ItemID: step.ItemID, Day: day, Channel: appmodel.CRMChannelPush}
			pushed, err := s.store.ClaimCRMReminder(ctx, claim)
			if err != nil {
				return err
			}
			if pushed {
				title, body := crmPushText(lang, step)
				if err := s.sender.Send(ctx, Notification{
					TeamID: key.teamID, UserIDs: []int64{key.userID}, Topic: TopicCRMNextStep,
					Title: title, Body: body, URL: "/crm",
				}); err != nil {
					return err
				}
			}
			claim.Channel = appmodel.CRMChannelEmail
			lettered, err := s.store.ClaimCRMReminder(ctx, claim)
			if err != nil {
				return err
			}
			if lettered {
				mailed = append(mailed, step)
			}
		}
		if len(mailed) == 0 {
			continue
		}
		// One failed mailbox must not hold back the other owners' letters, so the
		// error is kept and the loop goes on.
		subject, text := crmLetterText(lang, mailed, s.publicURL)
		if err := s.audience.QueueLetter(ctx, key.teamID, key.userID, subject, text); err != nil {
			mailErrs = append(mailErrs, err)
		}
	}
	return errors.Join(mailErrs...)
}

// workspaceLocation is the zone a workspace keeps its days in. A blank zone is UTC,
// the fallback the rest of the workspace calendar uses.
func workspaceLocation(zone string) (*time.Location, error) {
	if zone == "" {
		return time.UTC, nil
	}
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return nil, fmt.Errorf("load workspace time zone %q: %w", zone, err)
	}
	return loc, nil
}

func crmPushText(lang i18n.Lang, step appmodel.CRMStep) (string, string) {
	return i18n.T(lang, "crm.next.pushTitle"), crmItemLine(lang, step)
}

// crmLetterText lists every item due in one letter. The letter names the item and
// its date and links to /crm; it carries no token, so it is safe to queue.
func crmLetterText(lang i18n.Lang, steps []appmodel.CRMStep, publicURL string) (string, string) {
	lines := make([]string, 0, len(steps))
	for _, step := range steps {
		lines = append(lines, "- "+crmItemLine(lang, step))
	}
	text := i18n.T(lang, "crm.next.mailIntro") + "\n\n" + strings.Join(lines, "\n") +
		"\n\n" + i18n.T(lang, "crm.next.mailOpen") + " " + publicURL + "/crm"
	return i18n.T(lang, "crm.next.mailSubject"), text
}

func crmItemLine(lang i18n.Lang, step appmodel.CRMStep) string {
	kind := i18n.T(lang, "crm.next.kind.deal")
	if step.Kind == appmodel.CRMItemLead {
		kind = i18n.T(lang, "crm.next.kind.lead")
	}
	return fmt.Sprintf(i18n.T(lang, "crm.next.item"), kind, step.Name, step.NextStepAt)
}
