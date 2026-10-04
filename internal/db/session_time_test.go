package db

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

func TestAddSessionSecondsRejectsUnrepresentableDuration(t *testing.T) {
	maxSeconds := int64(math.MaxInt64) / int64(time.Second)
	maxInt := int64(int(^uint(0) >> 1))
	if maxSeconds > maxInt {
		maxSeconds = maxInt
	}
	if got, err := addSessionSeconds(maxSeconds, 0); err != nil || got != maxSeconds {
		t.Fatalf("addSessionSeconds(max, 0) = %d, %v; want %d, nil", got, err, maxSeconds)
	}
	if _, err := addSessionSeconds(maxSeconds, 1); !errors.Is(err, model.ErrSessionDurationOverflow) {
		t.Fatalf("addSessionSeconds(max, 1) error = %v, want ErrSessionDurationOverflow", err)
	}
	if _, err := addSessionSeconds(-1, 0); !errors.Is(err, appmodel.ErrInvalidSessionLength) {
		t.Fatalf("addSessionSeconds(-1, 0) error = %v, want ErrInvalidSessionLength", err)
	}
}
