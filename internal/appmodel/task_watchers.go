package appmodel

// PushNotifyRequest sends one push to people the caller names. The topic is the one a person
// can switch off (TASK-12).
type PushNotifyRequest struct {
	TeamID  int64
	UserIDs []int64
	Title   string
	Body    string
	URL     string
	Topic   string
}

// TaskWatchRequest sets whether a person watches a task, and with what role. A person may watch
// or stop watching a task themselves; only a manager adds someone else or names a co-assignee.
type TaskWatchRequest struct {
	TeamID   int64
	CallerID int64
	TaskID   int64
	UserID   int64
	Role     string
	Watch    bool
}

// TaskWatcher is a person who follows a task, as a watcher or a co-assignee.
type TaskWatcher struct {
	UserID int64
	Name   string
	Role   string
}
