package web

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/i18n"
)

// taskAttachmentView is one file on the task page (TASK-13).
type taskAttachmentView struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Size   string `json:"size"`
	Author string `json:"author"`
	When   string `json:"when"`
	URL    string `json:"url"`
}

type taskAttachmentCreatedResponse struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

func (taskAttachmentCreatedResponse) isJSONResponse() {}

// handleTaskAttachmentAdd stores a file with a task. The kind of file is decided from its bytes:
// an upload that claims to be a picture but is HTML is refused (TASK-13).
func (s *Server) handleTaskAttachmentAdd(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		s.writeJSONStatus(w, http.StatusNotFound, apiErrorResponse{Error: "not found"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, appmodel.MaxTaskAttachmentBytes+(64<<10))
	if err := r.ParseMultipartForm(appmodel.MaxTaskAttachmentBytes); err != nil {
		s.writeJSONStatus(w, http.StatusRequestEntityTooLarge, apiErrorResponse{Error: i18n.T(resolveLang(r), "tsa.tooLarge")})
		return
	}
	upload, header, err := r.FormFile("file")
	if err != nil {
		s.writeJSONStatus(w, http.StatusBadRequest, apiErrorResponse{Error: i18n.T(resolveLang(r), "tsa.noFile")})
		return
	}
	defer upload.Close()
	data, err := io.ReadAll(io.LimitReader(upload, appmodel.MaxTaskAttachmentBytes+1))
	if err != nil {
		s.writeInternalJSONError(w, err)
		return
	}
	if len(data) > appmodel.MaxTaskAttachmentBytes {
		s.writeJSONStatus(w, http.StatusRequestEntityTooLarge, apiErrorResponse{Error: i18n.T(resolveLang(r), "tsa.tooLarge")})
		return
	}
	contentType := http.DetectContentType(data)
	if !appmodel.AllowedTaskAttachmentType(contentType) {
		s.writeJSONStatus(w, http.StatusUnsupportedMediaType, apiErrorResponse{Error: i18n.T(resolveLang(r), "tsa.badType")})
		return
	}
	name := filepath.Base(strings.TrimSpace(header.Filename))
	info, err := s.services.Tasks.AddTaskAttachment(r.Context(), appmodel.TaskAttachmentAddRequest{
		TeamID: teamID(r), TaskID: id, CallerID: authenticatedUserID(r), Name: name, ContentType: contentType, Data: data,
	})
	if err != nil {
		if errors.Is(err, appmodel.ErrTaskAttachmentType) {
			s.writeJSONStatus(w, http.StatusUnsupportedMediaType, apiErrorResponse{Error: i18n.T(resolveLang(r), "tsa.badType")})
			return
		}
		s.writeTaskError(w, r, err)
		return
	}
	s.writeJSON(w, taskAttachmentCreatedResponse{ID: info.ID, Name: info.Name})
}

// handleTaskAttachmentGet sends a file as a download. It is never rendered in the page: the
// type is the one the bytes had, the disposition is attachment, and nosniff stops a browser
// from guessing another type (TASK-13).
func (s *Server) handleTaskAttachmentGet(w http.ResponseWriter, r *http.Request) {
	taskID, attachmentID, ok := s.taskAttachmentIDs(w, r)
	if !ok {
		return
	}
	file, err := s.services.Tasks.GetTaskAttachment(r.Context(), teamID(r), taskID, attachmentID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", file.ContentType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": file.Name}))
	w.Header().Set("Content-Length", strconv.Itoa(len(file.Data)))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, no-cache")
	_, _ = w.Write(file.Data)
}

// handleTaskAttachmentDelete removes a file; its author or a manager may do it (TASK-13).
func (s *Server) handleTaskAttachmentDelete(w http.ResponseWriter, r *http.Request) {
	taskID, attachmentID, ok := s.taskAttachmentIDs(w, r)
	if !ok {
		return
	}
	if err := s.services.Tasks.DeleteTaskAttachment(r.Context(), appmodel.TaskAttachmentDeleteRequest{
		TeamID: teamID(r), TaskID: taskID, CallerID: authenticatedUserID(r), AttachmentID: attachmentID,
	}); err != nil {
		s.writeTaskError(w, r, err)
		return
	}
	s.writeJSON(w, taskChecklistOKResponse{OK: true})
}

// taskAttachmentIDs reads the task and the file from the path; a bad id answers 404.
func (s *Server) taskAttachmentIDs(w http.ResponseWriter, r *http.Request) (int64, int64, bool) {
	taskID, err1 := strconv.ParseInt(r.PathValue("id"), 10, 64)
	attachmentID, err2 := strconv.ParseInt(r.PathValue("att"), 10, 64)
	if err1 != nil || err2 != nil || taskID <= 0 || attachmentID <= 0 {
		http.NotFound(w, r)
		return 0, 0, false
	}
	return taskID, attachmentID, true
}

// taskAttachmentViews lists a task's files for its page.
func (s *Server) taskAttachmentViews(r *http.Request, taskID int64) ([]taskAttachmentView, error) {
	files, err := s.services.Tasks.ListTaskAttachments(r.Context(), teamID(r), taskID)
	if err != nil {
		return nil, err
	}
	out := make([]taskAttachmentView, 0, len(files))
	for _, f := range files {
		out = append(out, taskAttachmentView{
			ID: f.ID, Name: f.Name, Size: attachmentSize(resolveLang(r), f.Size), Author: f.Author,
			When: fmtWhen(resolveLang(r), f.CreatedAt.In(userLoc(r)), userNow(r)),
			URL:  fmt.Sprintf("/tasks/%d/attachments/%d", taskID, f.ID),
		})
	}
	return out, nil
}

// attachmentSize prints a file's size in kilobytes or megabytes.
func attachmentSize(lang i18n.Lang, bytes int64) string {
	if bytes < 1<<20 {
		return fmt.Sprintf("%d %s", (bytes+1023)/1024, i18n.T(lang, "tsa.kb"))
	}
	return fmt.Sprintf("%.1f %s", float64(bytes)/(1<<20), i18n.T(lang, "tsa.mb"))
}
