package cli

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@x", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@x")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// The hook is written once, refuses to replace a hook it did not write, and
// on checking out a branch focuses the timer on the branch's last name part.
func TestGitHookFocusesTheBranch(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	repo := t.TempDir()
	git(t, repo, "init", "-q", "-b", "main")
	git(t, repo, "commit", "-q", "--allow-empty", "-m", "start")
	t.Chdir(repo)

	var out bytes.Buffer
	rt := NewRuntime(nil, &out, &out)
	if err := RunWithRuntime([]string{"hook", "install"}, rt); err != nil {
		t.Fatalf("install: %v", err)
	}
	hook := filepath.Join(repo, ".git", "hooks", "post-checkout")
	info, err := os.Stat(hook)
	if err != nil || info.Mode()&0o111 == 0 {
		t.Fatalf("hook not installed as an executable: %v", err)
	}

	// Run the script as git would, with a stand-in for the binary that
	// records what it was asked to do.
	record := filepath.Join(t.TempDir(), "called")
	stub := filepath.Join(t.TempDir(), "paratrack-stub")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\necho \"$@\" > "+record+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hook, []byte(hookScript(stub)), 0o755); err != nil {
		t.Fatal(err)
	}
	// A branch name is not trusted text: anything but letters, digits, dot,
	// dash and underscore becomes a dash before it reaches the server.
	git(t, repo, "checkout", "-q", "-b", "feat/PT-123-login")
	var called []byte
	// The hook runs the binary in the background; the shell creates the file
	// before it writes to it, so wait for the content, not just the file.
	for i := 0; i < 400; i++ {
		if called, err = os.ReadFile(record); err == nil && len(called) > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if strings.TrimSpace(string(called)) != "focus --branch feat/PT-123-login" {
		t.Fatalf("the hook called %q, want focus on the branch", called)
	}

	// Back on main the timers pause: reviewing there is not the feature's time.
	if err := os.Remove(record); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "checkout", "-q", "main")
	called = nil
	for i := 0; i < 400; i++ {
		if called, err = os.ReadFile(record); err == nil && len(called) > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	// The binary decides that main pauses; the script hands it the branch.
	if strings.TrimSpace(string(called)) != "focus --branch main" {
		t.Fatalf("checking out main called %q, want the branch handed over", called)
	}

	// Someone else's hook is left alone, both ways.
	if err := os.WriteFile(hook, []byte("#!/bin/sh\necho mine\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := RunWithRuntime([]string{"hook", "install"}, rt); err == nil {
		t.Fatal("installed over someone else's hook")
	}
	if err := RunWithRuntime([]string{"hook", "uninstall"}, rt); err == nil {
		t.Fatal("removed someone else's hook")
	}
	if err := os.WriteFile(hook, []byte(hookScript("paratrack")), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := RunWithRuntime([]string{"hook", "uninstall"}, rt); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if _, err := os.Stat(hook); !os.IsNotExist(err) {
		t.Fatal("the hook is still there")
	}

	// A hook manager owns core.hooksPath: the command explains instead of
	// writing a file into a folder that is committed or overwritten.
	git(t, repo, "config", "core.hooksPath", ".husky")
	if err := RunWithRuntime([]string{"hook", "install"}, rt); err == nil || !strings.Contains(err.Error(), "core.hooksPath") {
		t.Fatalf("installed under a hook manager: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, ".husky", "post-checkout")); !os.IsNotExist(err) {
		t.Fatal("a hook was written into the hook manager's folder")
	}
}

// The bash completion offers server-supplied names as plain text: a name
// holding $(...) is quoted, never run, and a name with a space stays one word.
func TestBashCompletionDoesNotRunActivityNames(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not installed")
	}
	work := t.TempDir()
	bin := t.TempDir()
	stub := "#!/bin/sh\nif [ \"$1\" = __activities ]; then printf '%s\\n' '$(touch PWNED)' 'Code review'; fi\n"
	if err := os.WriteFile(filepath.Join(bin, "paratrack"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	var script bytes.Buffer
	if err := RunWithRuntime([]string{"completion", "bash"}, NewRuntime(nil, &script, &script)); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bash, "-c", script.String()+`
COMP_WORDS=(paratrack start ""); COMP_CWORD=2; _paratrack; printf '%s\n' "${COMPREPLY[@]}"`)
	cmd.Dir = work
	cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("completion: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(work, "PWNED")); !os.IsNotExist(err) {
		t.Fatal("an activity name ran as a command")
	}
	got := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(got) != 2 || got[0] != `\$\(touch\ PWNED\)` || got[1] != `Code\ review` {
		t.Fatalf("offered %q", got)
	}
}

// The completion scripts load in each shell's own syntax, and the activities
// they offer come from the server, newest first.
func TestCompletion(t *testing.T) {
	var out bytes.Buffer
	rt := NewRuntime(nil, &out, &out)
	for shell, want := range map[string]string{"bash": "complete -F _paratrack", "zsh": "compdef _paratrack", "fish": "complete -c paratrack"} {
		out.Reset()
		if err := RunWithRuntime([]string{"completion", shell}, rt); err != nil || !strings.Contains(out.String(), want) {
			t.Fatalf("%s completion: %v\n%s", shell, err, out.String())
		}
	}
	if path, err := exec.LookPath("bash"); err == nil {
		out.Reset()
		_ = RunWithRuntime([]string{"completion", "bash"}, rt)
		if msg, err := exec.Command(path, "-n", "-c", out.String()).CombinedOutput(); err != nil {
			t.Fatalf("the bash script does not parse: %v\n%s", err, msg)
		}
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/me" {
			_, _ = io.WriteString(w, `{"email":"anton@studio.test"}`)
			return
		}
		// A manager's token sees the studio; the completion asks for its own.
		if r.URL.Query().Get("mine") == "1" {
			_, _ = io.WriteString(w, `{"sessions":[
			{"activity":"Ревью","start":"2026-11-27T10:00:00Z"},
			{"activity":"API","start":"2026-11-28T09:00:00Z"},
			{"activity":"Ревью","start":"2026-11-26T10:00:00Z"}]}`)
			return
		}
		_, _ = io.WriteString(w, `{"sessions":[
			{"activity":"Ревью","start":"2026-11-27T10:00:00Z","user_email":"anton@studio.test"},
			{"activity":"API","start":"2026-11-28T09:00:00Z","user_email":"anton@studio.test"},
			{"activity":"Бухгалтерия","start":"2026-11-28T11:00:00Z","user_email":"olga@studio.test"},
			{"activity":"Ревью","start":"2026-11-26T10:00:00Z","user_email":"anton@studio.test"}]}`)
	}))
	defer srv.Close()
	rt.RemoteURL, rt.RemoteToken, rt.HTTPClient = srv.URL, "pt_x", srv.Client()
	out.Reset()
	if err := RunWithRuntime([]string{"__activities"}, rt); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(out.String()); got != "API\nРевью" {
		t.Fatalf("activities offered: %q", got)
	}
}

// The activity name from a branch keeps letters of any alphabet and turns
// everything else into dashes.
func TestBranchActivity(t *testing.T) {
	for branch, want := range map[string]string{
		"feat/PT-123-login":       "PT-123-login",
		"feat/рус-ветка":          "рус-ветка",
		"feat/x$(touch_PWN)":      "x--touch_PWN",
		"PT-1;rm -rf":             "PT-1-rm--rf",
		"fix/Задача-12_правка.v2": "Задача-12_правка.v2",
		"PT-123/fix-login":        "PT-123-fix-login",
		"alice/PT-9-fix":          "PT-9-fix",
		"feat/🔥":                  "",
		"pt-123/fix-login":        "PT-123-fix-login",
		"feat/step-2-validation":  "step-2-validation",
		"fix/oauth2-1":            "oauth2-1",
	} {
		if got := branchActivity(branch, ""); got != want {
			t.Errorf("branchActivity(%q) = %q, want %q", branch, got, want)
		}
	}
}

// "hook print" is the script to paste into a hook manager's post-checkout.
func TestHookPrint(t *testing.T) {
	var out bytes.Buffer
	if err := RunWithRuntime([]string{"hook", "print"}, NewRuntime(nil, &out, &out)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), hookMarker) || !strings.Contains(out.String(), "focus --branch") {
		t.Fatalf("hook print:\n%s", out.String())
	}
}

// The script hands the binary the raw branch, an empty one for a detached
// HEAD, and the printed form names no path from this machine and never ends
// the hook it is pasted into.
func TestGitHookHandsOverTheBranchAndPrintsPortably(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	repo := t.TempDir()
	git(t, repo, "init", "-q", "-b", "main")
	git(t, repo, "commit", "-q", "--allow-empty", "-m", "start")
	t.Chdir(repo)
	record := filepath.Join(t.TempDir(), "called")
	stub := filepath.Join(t.TempDir(), "paratrack-stub")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\necho \"[$@]\" > "+record+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	hook := filepath.Join(repo, ".git", "hooks", "post-checkout")
	if err := os.MkdirAll(filepath.Dir(hook), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hook, []byte(hookScript(stub)), 0o755); err != nil {
		t.Fatal(err)
	}
	waitFor := func() string {
		for i := 0; i < 400; i++ {
			if called, err := os.ReadFile(record); err == nil && len(called) > 0 {
				_ = os.Remove(record)
				return strings.TrimSpace(string(called))
			}
			time.Sleep(20 * time.Millisecond)
		}
		return ""
	}
	git(t, repo, "checkout", "-q", "-b", "dev")
	if got := waitFor(); got != "[focus --branch dev]" {
		t.Fatalf("checking out dev called %q", got)
	}
	git(t, repo, "checkout", "-q", "--detach")
	if got := waitFor(); got != "[focus --branch ]" {
		t.Fatalf("a detached HEAD called %q", got)
	}
	var out bytes.Buffer
	if err := RunWithRuntime([]string{"hook", "print"}, NewRuntime(nil, &out, &out)); err != nil {
		t.Fatal(err)
	}
	printed := out.String()
	if !strings.Contains(printed, "command -v paratrack") || strings.Contains(printed, "bin='") {
		t.Fatalf("the printed hook names a local path:\n%s", printed)
	}
	for _, line := range strings.Split(printed, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "exit") {
			t.Fatalf("the printed hook ends the hook it is pasted into: %q", line)
		}
	}
}

// The binary pauses on the default and configured branches, globs included,
// on a detached HEAD and on a name with nothing usable; a broken ticket
// pattern is reported and the default used.
func TestBranchDecision(t *testing.T) {
	pauses := splitConfigList([]string{"dev  staging\tqa", "release/*,hotfix", "-develop"})
	for branch, want := range map[string]bool{
		"main": true, "dev": true, "qa": true, "release/1.2": true, "release/1.2/hotfix": true, "hotfix": true, "": true, "feat/🔥": true,
		"feat/PT-1-x": false, "releases": false, "develop": false,
	} {
		if pause, _, _ := branchDecision(branch, pauses, ""); pause != want {
			t.Errorf("branchDecision(%q) pause = %v, want %v", branch, pause, want)
		}
	}
	_, name, warn := branchDecision("feat/AB-12-x", nil, "[A-Z")
	if warn == nil || name != "AB-12-x" {
		t.Fatalf("a broken pattern: name %q, warn %v", name, warn)
	}
	if hookedBinary(hookScript("/opt/it's/paratrack")) != "/opt/it's/paratrack" {
		t.Fatal("a path with a quote does not read back")
	}
}

// The config is written by people and by git: a value in quotes, a value with
// a new line and several values of the same key all mean the same list.
func TestPauseListReadsQuotesAndNewLines(t *testing.T) {
	got := splitConfigList([]string{`"dev" staging`, "release/*\nhotfix", "qa, «нет»"})
	want := []string{"dev", "staging", "release/*", "hotfix", "qa", "«нет»"}
	if len(got) != len(want) {
		t.Fatalf("list = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("list = %q, want %q", got, want)
		}
	}
	if pause, _, _ := branchDecision("dev", got, ""); !pause {
		t.Fatal("a quoted branch name did not pause")
	}
}

// A ticket is a ticket in any case: the activity one person reaches from
// "feat/pt-123/fix-login" is the activity another reaches from
// "feat/PT-123/fix-login".
func TestTicketCaseLeadsTheSameName(t *testing.T) {
	for _, branch := range []string{"feat/pt-123/fix-login", "feat/PT-123/fix-login", "feat/pt-123-login"} {
		_, name, warn := branchDecision(branch, nil, "")
		if warn != nil {
			t.Fatalf("%s: %v", branch, warn)
		}
		if want := "PT-123-fix-login"; branch == "feat/pt-123-login" {
			want = "pt-123-login"
		} else if name != want {
			t.Fatalf("%s gave %q, want %q", branch, name, want)
		}
	}
	// A word with a number in it is not a key: the name is left as it reads.
	for _, branch := range []string{"fix/oauth2-1", "step-2-validation"} {
		want := branch[strings.LastIndex(branch, "/")+1:]
		if _, name, _ := branchDecision(branch, nil, ""); name != want {
			t.Fatalf("%s became %q, want the plain name %q", branch, name, want)
		}
	}
}

// The branch flag is a flag, not a first argument: `focus --name x --branch y`
// runs the branch's activity, and the flags beside it stay.
func TestBranchFlagIsFoundAnywhere(t *testing.T) {
	got, warn := branchCommand([]string{"focus", "--name", "old", "--branch", "feat/PT-42-login", "--note", "from hook"})
	if warn != nil {
		t.Fatalf("warn: %v", warn)
	}
	want := []string{"focus", "--name", "PT-42-login", "--note", "from hook"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] || got[3] != want[3] || got[4] != want[4] {
		t.Fatalf("command = %q, want %q", got, want)
	}
	// A pause branch drops the other flags with the activity.
	if got, _ := branchCommand([]string{"focus", "--note", "x", "--branch", "main"}); len(got) != 1 || got[0] != "pause" {
		t.Fatalf("a pause branch ran %q, want pause", got)
	}
	// No branch at all: the command is untouched.
	plain := []string{"focus", "--name", "design"}
	if got, _ := branchCommand(plain); len(got) != 3 || got[1] != "--name" || got[2] != "design" {
		t.Fatalf("a command without a branch became %q", got)
	}
}
