package web

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

// multipartUpload builds the body of a one-file upload form.
func multipartUpload(name string, data string) (string, string) {
	const boundary = "paratrack-test-boundary"
	body := "--" + boundary + "\r\n" +
		"Content-Disposition: form-data; name=\"file\"; filename=\"" + name + "\"\r\n" +
		"Content-Type: application/octet-stream\r\n\r\n" + data + "\r\n--" + boundary + "--\r\n"
	return body, "multipart/form-data; boundary=" + boundary
}

// A file on a task is kept as the kind its bytes say; HTML under a picture's name is refused, the
// download comes back as an attachment nothing renders, and only its author or a manager removes it
// (TASK-13).
func TestTaskAttachmentsKeepTheirRealKindAndWhoMayRemoveThem(t *testing.T) {
	owner := newAPIEnv(t)
	owner.register("attach-owner@x.test")
	member := newAPIEnvSharedDB(t, owner)
	member.register("attach-member@x.test")
	other := newAPIEnvSharedDB(t, owner)
	other.register("attach-other@x.test")
	joinStudio(t, owner, map[string]*apiEnv{"attach-member@x.test": member, "attach-other@x.test": other},
		"attach-member@x.test", "attach-other@x.test")

	readBody(t, owner.do("POST", "/projects/new", url.Values{"name": {"Файлы"}}, nil))
	readBody(t, owner.do("POST", "/api/tasks", url.Values{"project_id": {"1"}, "title": {"Макет"}}, map[string]string{"Accept": "application/json"}))
	var taskID int64
	if err := owner.db.TestSQL().QueryRowContext(t.Context(), `SELECT id FROM tasks WHERE title = 'Макет'`).Scan(&taskID); err != nil {
		t.Fatal(err)
	}
	task := strconv.FormatInt(taskID, 10)
	upload := "/api/tasks/" + task + "/attachments"

	png := "\x89PNG\r\n\x1a\n" + strings.Repeat("\x00", 32)
	body, ctype := multipartUpload("макет.png", png)
	added := postRaw(t, member, upload, ctype, body)
	readBody(t, added)
	if added.StatusCode != http.StatusOK {
		t.Fatalf("a picture was refused: %d", added.StatusCode)
	}

	html := "<html><script>alert(1)</script></html>"
	body, ctype = multipartUpload("pic.png", html)
	refused := postRaw(t, member, upload, ctype, body)
	readBody(t, refused)
	if refused.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("HTML under a picture's name was accepted: %d, want 415", refused.StatusCode)
	}

	var fileID int64
	if err := owner.db.TestSQL().QueryRowContext(t.Context(), `SELECT id FROM task_attachments ORDER BY id DESC LIMIT 1`).Scan(&fileID); err != nil {
		t.Fatal(err)
	}
	file := strconv.FormatInt(fileID, 10)
	download := owner.do("GET", "/tasks/"+task+"/attachments/"+file, nil, nil)
	readBody(t, download)
	if download.StatusCode != http.StatusOK || download.Header.Get("Content-Type") != "image/png" ||
		download.Header.Get("X-Content-Type-Options") != "nosniff" || !strings.HasPrefix(download.Header.Get("Content-Disposition"), "attachment") {
		t.Fatalf("download: status %d, type %q, disposition %q", download.StatusCode, download.Header.Get("Content-Type"), download.Header.Get("Content-Disposition"))
	}

	deletePath := "/api/tasks/" + task + "/attachments/" + file + "/delete"
	stranger := postRaw(t, other, deletePath, "application/x-www-form-urlencoded", "")
	readBody(t, stranger)
	if stranger.StatusCode != http.StatusForbidden {
		t.Fatalf("someone who did not attach the file removed it: %d, want 403", stranger.StatusCode)
	}

	removed := postRaw(t, owner, deletePath, "application/x-www-form-urlencoded", "")
	readBody(t, removed)
	if removed.StatusCode != http.StatusOK {
		t.Fatalf("the manager could not remove the file: %d", removed.StatusCode)
	}
	page := reactData[taskDetailPage](t, readBody(t, owner.do("GET", "/tasks/"+task, nil, nil)))
	if len(page.Attachments) != 0 {
		t.Fatalf("the task page still lists %+v after the removal", page.Attachments)
	}
}
