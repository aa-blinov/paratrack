package appmodel

import (
	"errors"
	"time"
)

// MaxTaskAttachmentBytes bounds one attached file (TASK-13).
const MaxTaskAttachmentBytes = 5 << 20

// ErrTaskAttachmentType means the bytes are not a kind of file a task may carry: images, PDF and plain text.
var ErrTaskAttachmentType = errors.New("file type is not allowed on a task")

// ErrTaskAttachmentSize means the file is empty or larger than MaxTaskAttachmentBytes.
var ErrTaskAttachmentSize = errors.New("file is empty or too large")

// AllowedTaskAttachmentType reports whether a detected content type may be attached to a task.
// Anything that can run or render as a page (HTML, SVG, scripts) is left out.
func AllowedTaskAttachmentType(contentType string) bool {
	switch contentType {
	case "image/png", "image/jpeg", "image/gif", "image/webp", "application/pdf", "text/plain; charset=utf-8":
		return true
	default:
		return false
	}
}

// TaskAttachmentInfo describes a file attached to a task, without its bytes (TASK-13).
type TaskAttachmentInfo struct {
	ID          int64
	TaskID      int64
	Name        string
	ContentType string
	Size        int64
	Author      string
	CreatedAt   time.Time
}

// TaskAttachmentFile is one attached file with its bytes, for download.
type TaskAttachmentFile struct {
	TaskAttachmentInfo
	Data []byte
}

// TaskAttachmentAddRequest attaches a file to a task. The content type is decided by the server
// from the bytes, not from what the upload claims (TASK-13).
type TaskAttachmentAddRequest struct {
	TeamID      int64
	TaskID      int64
	CallerID    int64
	Name        string
	ContentType string
	Data        []byte
}

// TaskAttachmentDeleteRequest removes one attached file. Its author or a manager may remove it.
type TaskAttachmentDeleteRequest struct {
	TeamID       int64
	TaskID       int64
	CallerID     int64
	AttachmentID int64
}
