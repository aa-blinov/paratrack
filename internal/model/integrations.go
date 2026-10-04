package model

// ExternalTask is a task imported from a connected provider.
type ExternalTask struct {
	ID            int64
	IntegrationID int64
	ExternalID    string
	Title         string
	URL           string
	Status        string
	ActivityID    int64
}
