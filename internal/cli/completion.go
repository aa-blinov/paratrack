package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
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
    local IFS=$'\n'
    COMPREPLY=($(compgen -W "$(paratrack __activities 2>/dev/null)" -- "$cur"))
  fi
}
complete -F _paratrack paratrack
`, words)
	case "zsh":
		fmt.Fprintf(rt.Out, `#compdef paratrack
# paratrack completion for zsh: eval "$(paratrack completion zsh)"
_paratrack() {
  if (( CURRENT == 2 )); then
    compadd -- %s
  elif (( CURRENT == 3 )) && [[ $words[2] == (start|focus) ]]; then
    local -a acts
    acts=("${(@f)$(paratrack __activities 2>/dev/null)}")
    compadd -- $acts
  fi
}
compdef _paratrack paratrack
`, words)
	case "fish":
		fmt.Fprintf(rt.Out, `# paratrack completion for fish: paratrack completion fish | source
complete -c paratrack -f -n "__fish_use_subcommand" -a "%s"
complete -c paratrack -f -n "__fish_seen_subcommand_from start focus" -a "(paratrack __activities 2>/dev/null)"
`, words)
	default:
		return &ExitError{Code: 2, Err: fmt.Errorf("unknown shell %q (bash, zsh or fish)", args[0])}
	}
	return nil
}

// runCompletionActivities prints recent activity names, newest first, one per
// line, for the shell to offer. Without a remote server it prints nothing:
// completion must never block or fail a prompt.
func runCompletionActivities(rt *Runtime) error {
	if !rt.remoteEnabled() {
		return nil
	}
	_, body, err := rt.remoteDo(http.MethodGet, "/api/v1/sessions", nil)
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
	for _, name := range names {
		fmt.Fprintln(rt.Out, name)
	}
	return nil
}
