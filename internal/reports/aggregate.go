package reports

import (
	"fmt"
	"sort"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/money"
)

// AggregateInput contains the data needed to build a reusable report. The
// adapter loads it and remains responsible for localized labels and output.
type AggregateInput struct {
	Sessions          []appmodel.ActiveSession
	Projects          map[int64]model.Project
	ProjectCurrencies map[int64]string
	UserNames         map[int64]string
	TeamCurrency      string
	Uncategorized     string
	UnassignedUser    string
	GroupBy           appmodel.ReportGroup
	Billable          bool
	// RoundSeconds applies the workspace rounding rule to one activity's time
	// before it is priced. Nil means no rounding.
	RoundSeconds func(seconds int) int
	From         time.Time
	To           time.Time
	Now          time.Time
	Location     *time.Location
}

type AggregateRow = appmodel.ReportAggregateRow
type AggregateResult = appmodel.ReportAggregateResult

type bucketKey struct {
	label    string
	day      string
	currency string
	userID   int64
}

// secondsPerHundredth is one hundredth of an hour: the precision of an hours
// figure on a report or an invoice line.
const secondsPerHundredth = 36

// billedLine is one activity's tracked time inside a bucket at its rate: the
// unit an invoice prices, so a report can price the same way. An activity at
// two rates is two lines, as two invoice lines would be.
type billedLine struct {
	seconds int
	rate    int
}

type lineKey struct {
	activityID int64
	rate       int
}

type bucket struct {
	seconds    int
	rate       int
	rateSet    bool
	rateVaries bool
	amount     int
	currency   string
	lines      map[lineKey]*billedLine
	day        time.Time
}

// Aggregate applies report grouping, tracked-time allocation, and billing
// arithmetic without depending on HTTP, templates, or localized formatting.
func Aggregate(input AggregateInput) (AggregateResult, error) {
	if !input.GroupBy.Valid() {
		return AggregateResult{}, ErrInvalidReportQuery
	}
	teamCurrency := input.TeamCurrency
	if teamCurrency == "" {
		teamCurrency = "RUB"
	}
	location := input.Location
	if location == nil {
		location = time.UTC
	}
	buckets := make(map[bucketKey]*bucket)
	result := AggregateResult{ByCurrency: make(map[string]int), TeamCurrency: teamCurrency}

	for _, session := range input.Sessions {
		seconds := session.Session.TrackedSecondsInWindow(input.From, input.To, input.Now)
		if seconds <= 0 {
			continue
		}
		label := session.Activity.Name
		var day time.Time
		identityDay := ""
		userID := int64(0)
		switch input.GroupBy {
		case appmodel.ReportGroupProject:
			if session.Activity.ProjectID == 0 {
				label = input.Uncategorized
			} else if project, ok := input.Projects[session.Activity.ProjectID]; ok {
				label = project.Name
			} else {
				label = input.Uncategorized
			}
		case appmodel.ReportGroupActivity:
			label = session.Activity.Name
		case appmodel.ReportGroupDay:
			day = session.Session.StartAt.In(location)
			identityDay = day.Format("2006-01-02")
			label = identityDay
		case appmodel.ReportGroupUser:
			userID = session.Session.UserID
			if userID == 0 {
				label = input.UnassignedUser
			} else if name, ok := input.UserNames[userID]; ok {
				label = name
			} else {
				label = input.UnassignedUser
			}
		}

		currency := teamCurrency
		if input.Billable && session.Activity.ProjectID > 0 {
			if projectCurrency := input.ProjectCurrencies[session.Activity.ProjectID]; projectCurrency != "" {
				currency = projectCurrency
			}
		}
		currencyKey := ""
		if input.Billable && currency != teamCurrency {
			currencyKey = currency
		}
		key := bucketKey{label: label, day: identityDay, currency: currencyKey, userID: userID}
		current := buckets[key]
		if current == nil {
			current = &bucket{currency: currency, day: day}
			buckets[key] = current
		}
		var err error
		current.seconds, err = money.AddInt(current.seconds, seconds)
		if err != nil {
			return AggregateResult{}, fmt.Errorf("sum report bucket %q: %w", label, err)
		}
		result.TotalSeconds, err = money.AddInt(result.TotalSeconds, seconds)
		if err != nil {
			return AggregateResult{}, fmt.Errorf("sum report tracked time: %w", err)
		}

		if input.Billable {
			rate := 0
			if project, ok := input.Projects[session.Activity.ProjectID]; ok && project.Billable && project.BillableRateCents != nil {
				rate = *project.BillableRateCents
			}
			if !current.rateSet {
				current.rate = rate
				current.rateSet = true
			} else if current.rate != rate {
				current.rateVaries = true
			}
			if current.lines == nil {
				current.lines = make(map[lineKey]*billedLine)
			}
			key := lineKey{activityID: session.Activity.ID, rate: rate}
			line := current.lines[key]
			if line == nil {
				line = &billedLine{rate: rate}
				current.lines[key] = line
			}
			line.seconds, err = money.AddInt(line.seconds, seconds)
			if err != nil {
				return AggregateResult{}, fmt.Errorf("sum report time for activity %d: %w", session.Activity.ID, err)
			}
		}
	}

	// Billable time is priced line by line, as an invoice prices it: each
	// activity's time goes through the workspace rounding rule, its hours to
	// hundredths, and the amount is the rate times those hours. A bucket's hours
	// are the sum of its lines, so the hours shown, the amount and an invoice
	// built from the same lines agree to the cent.
	if input.Billable {
		result.TotalSeconds = 0
		for _, current := range buckets {
			current.seconds = 0
			for _, line := range current.lines {
				rounded := line.seconds
				if input.RoundSeconds != nil {
					rounded = input.RoundSeconds(line.seconds)
				}
				current.seconds += money.HoursHundredths(rounded) * secondsPerHundredth
				amount, err := money.PriceCents(rounded, line.rate)
				if err != nil {
					return AggregateResult{}, err
				}
				current.amount, err = money.AddCents(current.amount, amount)
				if err != nil {
					return AggregateResult{}, err
				}
			}
			result.TotalSeconds += current.seconds
		}
	}

	result.Rows = make([]AggregateRow, 0, len(buckets))
	for key, current := range buckets {
		var err error
		result.TotalCents, err = money.AddCents(result.TotalCents, current.amount)
		if err != nil {
			return AggregateResult{}, err
		}
		result.ByCurrency[current.currency], err = money.AddCents(result.ByCurrency[current.currency], current.amount)
		if err != nil {
			return AggregateResult{}, err
		}
		share := 0.0
		if result.TotalSeconds > 0 {
			share = float64(current.seconds) / float64(result.TotalSeconds) * 100
		}
		rate := current.rate
		if current.rateVaries {
			rate = 0 // A grouped row must never present one constituent rate as its total rate.
		}
		result.Rows = append(result.Rows, AggregateRow{
			Key: key.label, UserID: key.userID, Day: current.day, Seconds: current.seconds,
			RateCents: rate, RateVaries: current.rateVaries, AmountCents: current.amount,
			Currency: current.currency, Share: share,
		})
	}
	sort.Slice(result.Rows, func(i, j int) bool {
		if result.Rows[i].Seconds == result.Rows[j].Seconds {
			if result.Rows[i].Key == result.Rows[j].Key {
				return result.Rows[i].Currency < result.Rows[j].Currency
			}
			return result.Rows[i].Key < result.Rows[j].Key
		}
		return result.Rows[i].Seconds > result.Rows[j].Seconds
	})
	return result, nil
}
