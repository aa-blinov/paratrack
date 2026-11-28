package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// completionCommands are the words the shell completes after "paratrack".
var completionCommands = []string{
	"status", "start", "stop", "pause", "resume", "focus", "add", "log", "stats",
	"goal", "tag", "project", "auth", "hook", "completion", "web", "help",
}

// runCompletion prints a completion script. Commands are completed from a
// fixed list; the activity after start and focus comes from
// "paratrack __activities", which asks the server for recent ones.
func runCompletion(rt *Runtime, args []string) error {
	if len(args) != 1 {
		return &ExitError{Code: 2, Err: errors.New("usage: paratrack completion bash|zsh|fish")}
	}
	words := strings.Join(completionCommands, " ")
	switch args[0] {
	case "bash":
		fmt.Fprintf(rt.Out, `# paratrack completion for bash: eval "$(paratrack completion bash)"
_paratrack() {
  local cur=${COMP_WORDS[COMP_CWORD]}
  if [ "$COMP_CWORD" -eq 1 ]; then
    COMPREPLY=($(compgen -W "%s" -- "$cur"))
  elif [ "$COMP_CWORD" -eq 2 ] && { [ "${COMP_WORDS[1]}" = start ] || [ "${COMP_WORDS[1]}" = focus ]; }; then
    # Activity names come from the server and are matched as plain text,
    # never through compgen -W, which would expand $(...) inside a name.
    COMPREPLY=()
    local line
    while IFS= read -r line; do
      [[ $line == "$cur"* ]] && COMPREPLY+=("$(printf '%%q' "$line")")
    done < <(paratrack __activities 2>/dev/null)
  fi
}
complete -F _paratrack paratrack
`, words)
	case "zsh":
		fmt.Fprintf(rt.Out, `#compdef paratrack
# paratrack completion for zsh: eval "$(paratrack completion zsh)"
# compdef comes from compinit, which a bare .zshrc has not loaded yet.
(( $+functions[compdef] )) || { autoload -Uz compinit && compinit; }
_paratrack() {
  if (( CURRENT == 2 )); then
    compadd -- %s
  elif (( CURRENT == 3 )) && [[ $words[2] == (start|focus) ]]; then
    local -a acts
    # (f) splits on lines and drops the empty ones, so no answer offers nothing.
    acts=(${(f)"$(paratrack __activities 2>/dev/null)"})
    (( ${#acts} )) && compadd -- $acts
  fi
}
compdef _paratrack paratrack
`, words)
	case "fish":
		fmt.Fprintf(rt.Out, `# paratrack completion for fish: paratrack completion fish | source
complete -c paratrack -f -n "__fish_use_subcommand" -a "%s"
complete -c paratrack -f -n "__fish_seen_subcommand_from start focus; and test (count (commandline -opc)) -eq 2" -a "(paratrack __activities 2>/dev/null)"
`, words)
	default:
		return &ExitError{Code: 2, Err: fmt.Errorf("unknown shell %q (bash, zsh or fish)", args[0])}
	}
	return nil
}

// completionTimeout bounds one TAB: the shell waits for this command.
const completionTimeout = 400 * time.Millisecond

// runCompletionActivities prints the caller's own recent activity names,
// newest first, one per line, for the shell to offer. A manager's token reads
// the whole studio's sessions, so they are filtered to the token owner. It
// gives up after completionTimeout and prints nothing rather than block or
// fail a prompt; without a remote server it prints nothing.
func runCompletionActivities(rt *Runtime) error {
	if !rt.remoteEnabled() {
		return nil
	}
	// A TAB pressed again within a minute answers from the last list.
	cache := rt.completionCachePath()
	if cache != "" {
		if info, err := os.Stat(cache); err == nil && rt.now().Sub(info.ModTime()) < completionCacheMaxAge {
			if body, err := os.ReadFile(cache); err == nil {
				_, _ = rt.Out.Write(body)
				return nil
			}
		}
	}
	ctx, cancel := context.WithTimeout(rt.Context, completionTimeout)
	defer cancel()
	// mine=1: the caller's own sessions only, so a manager's token is not
	// filled up by the studio's recent days.
	_, body, err := rt.remoteDoContext(ctx, http.MethodGet, "/api/v1/sessions?mine=1&limit=300", nil)
	if err != nil {
		return nil
	}
	var res struct {
		Sessions []struct {
			Activity string `json:"activity"`
			Start    string `json:"start"`
		} `json:"sessions"`
	}
	if json.Unmarshal(body, &res) != nil {
		return nil
	}
	latest := map[string]string{}
	for _, s := range res.Sessions {
		if s.Activity != "" && s.Start > latest[s.Activity] {
			latest[s.Activity] = s.Start
		}
	}
	names := make([]string, 0, len(latest))
	for name := range latest {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool { return latest[names[i]] > latest[names[j]] })
	if len(names) > 50 {
		names = names[:50]
	}
	var out strings.Builder
	for _, name := range names {
		out.WriteString(name + "\n")
	}
	fmt.Fprint(rt.Out, out.String())
	if cache != "" {
		writeFileAtomic(cache, []byte(out.String()))
		dropStaleActivityCaches(cache, rt.now())
	}
	return nil
}

// dropStaleActivityCaches removes the lists other tokens left more than a day
// ago, so a token used once does not leave a file behind for good.
func dropStaleActivityCaches(keep string, now time.Time) {
	matches, err := filepath.Glob(strings.TrimSuffix(keep, filepath.Ext(keep)) + "*")
	if err != nil {
		return
	}
	prefix := keep[:strings.LastIndex(keep, ".activities-")+len(".activities-")]
	for _, path := range matches {
		if path == keep || !strings.HasPrefix(path, prefix) {
			continue
		}
		if info, err := os.Stat(path); err == nil && now.Sub(info.ModTime()) > 24*time.Hour {
			_ = os.Remove(path)
		}
	}
}

// completionCacheMaxAge is how long a list of activities answers a TAB.
const completionCacheMaxAge = time.Minute

// completionCachePath sits next to the prompt line's cache and, like it, is
// keyed by the server and token, so another account never sees this list.
func (rt *Runtime) completionCachePath() string {
	if strings.TrimSpace(rt.StatusCachePath) == "" {
		return ""
	}
	return rt.StatusCachePath + ".activities-" + rt.statusCacheOwner()
}

// writeFileAtomic writes aside and renames into place, best effort: a TAB in
// another terminal never reads half a list.
func writeFileAtomic(path string, data []byte) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".activities-*")
	if err != nil {
		return
	}
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if werr != nil || cerr != nil || os.Chmod(tmp.Name(), 0o600) != nil || os.Rename(tmp.Name(), path) != nil {
		_ = os.Remove(tmp.Name())
	}
}
