package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode"
)

// hookMarker identifies the post-checkout hook this command wrote, so it never
// overwrites or removes a hook somebody else put there.
const hookMarker = "# paratrack-hook"

// hookScript switches the timer to the branch that was checked out. The
// script only finds the binary and settles quick checkouts; the binary
// decides the rest from the raw branch (see branchDecision): a branch name is
// not trusted text, git config is read with every value and separator, and a
// mistake in the ticket pattern is told, not swallowed. Quick successive
// checkouts are settled after a short pause, and only the branch still
// checked out is sent. It runs in the background and never fails the
// checkout, and it has no "exit" outside its subshell, so pasted into the
// middle of a hook manager's post-checkout it does not end the commands after
// it.
//
// With an empty binary the script finds paratrack on PATH when it runs, and
// in the usual install folders a desktop git client leaves off its PATH: the
// form for a hook manager's folder, committed and shared, where an absolute
// path from one laptop would fail silently on every other.
func hookScript(binary string) string {
	find := `bin=""`
	if binary != "" {
		find = `bin='` + strings.ReplaceAll(binary, `'`, `'\''`) + `'
  [ -x "$bin" ] || bin=""`
	}
	return `#!/bin/sh
` + hookMarker + ` — written by "paratrack hook install", removed by "paratrack hook uninstall".
# Switches the paratrack timer to the branch you check out.
if [ "$3" = "1" ]; then
  ` + find + `
  [ -n "$bin" ] || bin=$(command -v paratrack 2>/dev/null) || bin=""
  for dir in /opt/homebrew/bin /usr/local/bin "$HOME/go/bin" "$HOME/.local/bin" "$HOME/.local/share/mise/shims" "$HOME/.asdf/shims"; do
    [ -n "$bin" ] || { [ -x "$dir/paratrack" ] && bin="$dir/paratrack"; }
  done
  if [ -n "$bin" ]; then
    branch=$(git symbolic-ref --quiet --short HEAD 2>/dev/null) || branch=""
    mark=$(git rev-parse --git-path paratrack-focus)
    printf '%s' "${branch:-(detached)}" > "$mark"
    ( sleep 1
      [ "$(cat "$mark" 2>/dev/null)" = "${branch:-(detached)}" ] || exit 0
      now=$(git symbolic-ref --quiet --short HEAD 2>/dev/null) || now=""
      [ "$now" = "$branch" ] || exit 0
      "$bin" focus --branch "$branch" >/dev/null 2>"$(git rev-parse --git-path paratrack-hook.log)" ) &
  fi
fi
`
}

func runHook(rt *Runtime, args []string) error {
	if len(args) == 0 || (args[0] != "install" && args[0] != "uninstall" && args[0] != "show" && args[0] != "print") {
		return &ExitError{Code: 2, Err: errors.New("usage: paratrack hook install|uninstall|show|print")}
	}
	// print gives the same script to paste into a hook manager's
	// post-checkout (husky, lefthook), which owns the hooks folder.
	if args[0] == "print" {
		fmt.Fprint(rt.Out, hookScript(""))
		return nil
	}
	dir, err := gitHooksDir()
	if err != nil {
		return err
	}
	path := filepath.Join(dir, "post-checkout")
	current, readErr := os.ReadFile(path)
	ours := readErr == nil && strings.Contains(string(current), hookMarker)
	switch args[0] {
	case "show":
		if readErr != nil {
			fmt.Fprintln(rt.Out, "no post-checkout hook")
			return nil
		}
		fmt.Fprint(rt.Out, string(current))
		// The hook runs in the background with its output away from the
		// terminal; what the binary had to say last time is kept for here.
		if out, err := exec.Command("git", "rev-parse", "--git-path", "paratrack-hook.log").Output(); err == nil {
			if log, err := os.ReadFile(strings.TrimSpace(string(out))); err == nil && len(strings.TrimSpace(string(log))) > 0 {
				fmt.Fprintf(rt.Out, "\nlast checkout said:\n%s", log)
			}
		}
		if ours {
			if bin := hookedBinary(string(current)); bin != "" {
				if _, err := os.Stat(bin); err != nil {
					fmt.Fprintf(rt.Out, "\n! %s is gone (moved or upgraded); run: paratrack hook install\n", bin)
				}
			}
		}
		return nil
	case "uninstall":
		if readErr != nil {
			fmt.Fprintln(rt.Out, "no post-checkout hook")
			return nil
		}
		if !ours {
			return fmt.Errorf("%s was not written by paratrack; leaving it as it is", path)
		}
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("remove hook: %w", err)
		}
		fmt.Fprintln(rt.Out, "✓ removed", path)
		return nil
	}
	if readErr == nil && !ours {
		return fmt.Errorf("%s already exists and was not written by paratrack; paste the output of \"paratrack hook print\" into it instead", path)
	}
	// A hook manager (husky, lefthook) owns core.hooksPath, often a tracked
	// folder: a file written there would be committed or overwritten.
	if out, err := exec.Command("git", "config", "--get", "core.hooksPath").Output(); err == nil && strings.TrimSpace(string(out)) != "" {
		return fmt.Errorf("hooks are managed through core.hooksPath (%s); paste the output of \"paratrack hook print\" into your post-checkout hook there", strings.TrimSpace(string(out)))
	}
	binary := hookBinary()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create hooks directory: %w", err)
	}
	if err := os.WriteFile(path, []byte(hookScript(binary)), 0o755); err != nil {
		return fmt.Errorf("write hook: %w", err)
	}
	fmt.Fprintln(rt.Out, "✓ installed", path)
	fmt.Fprintln(rt.Out, "  checking out a branch now focuses the timer on its last name part (feat/PT-123-login → PT-123-login)")
	fmt.Fprintln(rt.Out, "  git config paratrack.pauseBranches \"dev staging\" adds branches that pause; paratrack.ticketPattern keeps a ticket")
	return nil
}

// gitHooksDir asks git where this repository keeps its hooks, which honours
// worktrees and core.hooksPath.
func gitHooksDir() (string, error) {
	out, err := exec.Command("git", "rev-parse", "--git-path", "hooks").Output()
	if err != nil {
		return "", errors.New("not inside a git repository")
	}
	dir := strings.TrimSpace(string(out))
	if !filepath.IsAbs(dir) {
		wd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(wd, dir)
	}
	return dir, nil
}

// hookBinary is the paratrack the hook calls: the one on PATH when there is
// one, which survives an upgrade that moves the install, or else this binary.
func hookBinary() string {
	if path, err := exec.LookPath("paratrack"); err == nil {
		if abs, err := filepath.Abs(path); err == nil {
			return abs
		}
		return path
	}
	if exe, err := os.Executable(); err == nil {
		return exe
	}
	return "paratrack"
}

// hookedBinary reads back the binary path an installed hook calls, empty
// when it finds paratrack on PATH. A quote in the path is written '\” and
// read back as one.
func hookedBinary(script string) string {
	for _, line := range strings.Split(script, "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "bin='"); ok {
			if strings.HasSuffix(rest, "'") {
				return strings.ReplaceAll(strings.TrimSuffix(rest, "'"), `'\''`, `'`)
			}
		}
	}
	return ""
}

// defaultTicketPattern finds a tracker key in capitals (PT-123) anywhere in
// a branch. A key in lower case counts only as a whole part before the last
// ("pt-123/fix-login"): "step-2-validation" and "fix/oauth2-1" are words.
var defaultTicketPattern = regexp.MustCompile(`\b[A-Z][A-Z0-9]+-[0-9]+\b`)

// lowerTicketPart is a branch part that is a key in lower case and nothing else.
var lowerTicketPart = regexp.MustCompile(`^[a-z][a-z0-9]*-[0-9]+$`)

// defaultPauseBranches pause the timers when checked out.
var defaultPauseBranches = []string{"main", "master", "develop", "trunk"}

// branchDecision says what checking out a branch does: pause (a branch in the
// pause list, by name or glob such as release/*, a detached HEAD, or a name
// with nothing usable in it), or focus on the activity named. A ticket
// pattern that does not compile is reported and the default one used.
func branchDecision(branch string, pauses []string, pattern string) (pause bool, name string, warn error) {
	if branch == "" {
		return true, "", nil
	}
	// "-main" in the list takes a default back out ("-develop" for a team
	// that works on develop); "release/*" matches every depth below release/.
	list := append([]string(nil), defaultPauseBranches...)
	for _, glob := range pauses {
		if removed, ok := strings.CutPrefix(glob, "-"); ok {
			list = slices.DeleteFunc(list, func(g string) bool { return g == removed })
			continue
		}
		list = append(list, glob)
	}
	for _, glob := range list {
		if prefix, ok := strings.CutSuffix(glob, "/*"); ok && strings.HasPrefix(branch, prefix+"/") {
			return true, "", nil
		}
		if ok, err := path.Match(glob, branch); err == nil && ok {
			return true, "", nil
		} else if err != nil && warn == nil {
			warn = fmt.Errorf("paratrack.pauseBranches: %q is not a valid pattern", glob)
		}
	}
	if pattern != "" {
		if _, err := regexp.Compile(pattern); err != nil {
			warn = fmt.Errorf("paratrack.ticketPattern %q does not compile: %v", pattern, err)
			pattern = ""
		}
	}
	name = branchActivity(branch, pattern)
	return name == "", name, warn
}

// splitConfigList reads a git config list given as one or more values, each
// separated by spaces, tabs or commas.
func splitConfigList(values []string) []string {
	var out []string
	for _, value := range values {
		out = append(out, strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' })...)
	}
	return out
}

// gitConfigAll reads every value of a git config key in the current repository.
func gitConfigAll(key string) []string {
	out, err := exec.Command("git", "config", "--get-all", key).Output()
	if err != nil {
		return nil
	}
	return strings.Split(strings.TrimRight(string(out), "\n"), "\n")
}

// branchActivity makes an activity name from a git branch: its last part,
// with letters and digits of any alphabet, dot, dash and underscore kept and
// anything else (quotes, $, spaces, control characters) turned into a dash.
// A ticket found earlier in the branch ("PT-123/fix-login") leads the name,
// so it is not lost; pattern, when given, is the team's own ticket form.
func branchActivity(branch, pattern string) string {
	ticket := defaultTicketPattern
	if pattern != "" {
		if custom, err := regexp.Compile(pattern); err == nil {
			ticket = custom
		}
	}
	last := branch
	if i := strings.LastIndex(branch, "/"); i >= 0 {
		last = branch[i+1:]
	}
	found := ticket.FindString(branch)
	if found == "" && pattern == "" {
		parts := strings.Split(branch, "/")
		for _, part := range parts[:len(parts)-1] {
			if lowerTicketPart.MatchString(part) {
				found = part
				break
			}
		}
	}
	if found != "" {
		if i := strings.Index(strings.ToLower(last), strings.ToLower(found)); i >= 0 {
			last = last[:i] + strings.ToUpper(found) + last[i+len(found):]
		} else {
			last = strings.ToUpper(found) + "-" + last
		}
	}
	branch = last
	var b strings.Builder
	for _, r := range branch {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '.' || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-")
}
