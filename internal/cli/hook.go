package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
)

// hookMarker identifies the post-checkout hook this command wrote, so it never
// overwrites or removes a hook somebody else put there.
const hookMarker = "# paratrack-hook"

// hookScript switches the timer to the branch that was checked out. The
// branch goes to "focus --branch", which makes the activity name in Go (see
// branchActivity): a branch name is not trusted text, and the shell's tr
// cannot tell UTF-8 letters from noise. It runs in the background and never
// fails the checkout. Quick successive checkouts are settled after a short
// pause, and only the branch still checked out is sent, so focus calls
// arriving out of order cannot leave the timer on a branch already left.
// main, master, develop, trunk, the branches named in git config
// paratrack.pauseBranches and a detached HEAD pause the timers instead:
// reviewing there is not the feature's time. git config
// paratrack.ticketPattern names the ticket to keep from the branch.
//
// With an empty binary the script finds paratrack on PATH when it runs: the
// form for a hook manager's folder, which is committed and shared, where an
// absolute path from one laptop would fail silently on every other.
func hookScript(binary string) string {
	find := `bin=$(command -v paratrack) || exit 0`
	if binary != "" {
		find = `bin='` + strings.ReplaceAll(binary, `'`, `'\''`) + `'
[ -x "$bin" ] || bin=$(command -v paratrack) || exit 0`
	}
	return `#!/bin/sh
` + hookMarker + ` — written by "paratrack hook install", removed by "paratrack hook uninstall".
# Switches the paratrack timer to the branch you check out.
[ "$3" = "1" ] || exit 0
` + find + `
branch=$(git symbolic-ref --quiet --short HEAD 2>/dev/null) || branch=""
mark=$(git rev-parse --git-path paratrack-focus)
printf '%s' "${branch:-(detached)}" > "$mark"
( sleep 1
  [ "$(cat "$mark" 2>/dev/null)" = "${branch:-(detached)}" ] || exit 0
  now=$(git symbolic-ref --quiet --short HEAD 2>/dev/null) || now=""
  [ "$now" = "$branch" ] || exit 0
  [ -n "$branch" ] || { "$bin" pause >/dev/null 2>&1; exit 0; }
  case " main master develop trunk $(git config --get paratrack.pauseBranches) " in
    *" $branch "*) "$bin" pause >/dev/null 2>&1 ;;
    *) "$bin" focus --branch "$branch" --pattern "$(git config --get paratrack.ticketPattern)" >/dev/null 2>&1 ;;
  esac ) &
exit 0
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
// when it finds paratrack on PATH.
func hookedBinary(script string) string {
	for _, line := range strings.Split(script, "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "bin='"); ok {
			if end := strings.Index(rest, "'"); end >= 0 {
				return rest[:end]
			}
		}
	}
	return ""
}

// defaultTicketPattern finds a tracker key such as PT-123 anywhere in a branch.
var defaultTicketPattern = regexp.MustCompile(`[A-Z][A-Z0-9]+-[0-9]+`)

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
	if found := ticket.FindString(branch); found != "" && !strings.Contains(last, found) {
		last = found + "-" + last
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
