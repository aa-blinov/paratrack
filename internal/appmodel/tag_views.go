package appmodel

import "github.com/aa-blinov/paratrack/internal/model"

// TagWithCount is a tag and the number of sessions that use it.
type TagWithCount struct {
	model.Tag
	SessionCount int
}
