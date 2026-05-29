// Package commandpolicy classifies shell commands for safety policy decisions.
package commandpolicy

import (
	"regexp"
	"strings"
)

// Action describes the required handling for a shell command.
type Action string

const (
	// ActionAllow means no additional command-specific restriction is needed.
	ActionAllow Action = "allow"
	// ActionRequireApproval means the command is allowed only after explicit approval.
	ActionRequireApproval Action = "require_approval"
	// ActionDeny means the command must never be executed.
	ActionDeny Action = "deny"
)

// Decision is the command-specific safety classification.
type Decision struct {
	Action Action
	Reason string
	Risk   string
}

type commandPattern struct {
	pattern *regexp.Regexp
	reason  string
}

var neverRunPatterns = []commandPattern{
	{regexp.MustCompile(`(?i)(^|[;&|()[:space:]])rm[[:space:]]+[^;&|]*-([[:alnum:]]*r[[:alnum:]]*f|[[:alnum:]]*f[[:alnum:]]*r)[[:alnum:]]*[^;&|]*[[:space:]]+/(\*|[[:space:]]|$|[;&|])`), "refusing to recursively remove the root filesystem"},
	{regexp.MustCompile(`(?i)(^|[;&|()[:space:]])rm[[:space:]]+[^;&|]*-([[:alnum:]]*r[[:alnum:]]*f|[[:alnum:]]*f[[:alnum:]]*r)[[:alnum:]]*[^;&|]*[[:space:]]+/\*([[:space:]]|$|[;&|])`), "refusing to recursively remove root filesystem contents"},
	{regexp.MustCompile(`(?i)(^|[;&|()[:space:]])mkfs(\.[[:alnum:]_+-]+)?[[:space:]]+[^;&|]*(/dev/(sd[a-z][0-9]*|vd[a-z][0-9]*|xvd[a-z][0-9]*|nvme[0-9]+n[0-9]+p?[0-9]*|mapper/[[:alnum:]_.-]+))`), "refusing to format a block device"},
	{regexp.MustCompile(`(?i)(^|[;&|()[:space:]])dd[[:space:]]+[^;&|]*(of=/dev/(sd[a-z][0-9]*|vd[a-z][0-9]*|xvd[a-z][0-9]*|nvme[0-9]+n[0-9]+p?[0-9]*|mapper/[[:alnum:]_.-]+))`), "refusing to overwrite a block device"},
	{regexp.MustCompile(`(?i)(^|[;&|()[:space:]])shred[[:space:]]+[^;&|]*/dev/(sd[a-z][0-9]*|vd[a-z][0-9]*|xvd[a-z][0-9]*|nvme[0-9]+n[0-9]+p?[0-9]*|mapper/[[:alnum:]_.-]+)`), "refusing to shred a block device"},
	{regexp.MustCompile(`:\s*\(\s*\)\s*\{\s*:\s*\|\s*:\s*&\s*}\s*;\s*:`), "refusing to run a fork bomb"},
}

var approvalPatterns = []commandPattern{
	{regexp.MustCompile(`(?i)(^|[;&|()[:space:]])rm[[:space:]]+[^;&|]*-([[:alnum:]]*r[[:alnum:]]*f|[[:alnum:]]*f[[:alnum:]]*r)[[:alnum:]]*[[:space:]]+`), "recursive forced removal requires approval"},
	{regexp.MustCompile(`(?i)(^|[;&|()[:space:]])sudo[[:space:]]+`), "sudo command requires approval"},
	{regexp.MustCompile(`(?i)(^|[;&|()[:space:]])chmod[[:space:]]+[^;&|]*(777|-[[:alnum:]]*R[[:alnum:]]*)`), "broad chmod change requires approval"},
	{regexp.MustCompile(`(?i)(^|[;&|()[:space:]])chown[[:space:]]+[^;&|]*-[[:alnum:]]*R[[:alnum:]]*`), "recursive ownership change requires approval"},
	{regexp.MustCompile(`(?i)(^|[;&|()[:space:]])(systemctl[[:space:]]+(stop|restart|disable|mask)[[:space:]]+|service[[:space:]]+[^;&|[:space:]]+[[:space:]]+(stop|restart|disable|mask)([[:space:]]|$|[;&|]))`), "service control command requires approval"},
	{regexp.MustCompile(`(?i)(^|[;&|()[:space:]])(reboot|shutdown|poweroff|halt)([[:space:]]|$|[;&|])`), "system shutdown command requires approval"},
}

// EvaluateCommandParts classifies an already-split command. It unwraps common
// shell interpreter forms such as sh -c <command> so policy applies to the
// actual shell payload instead of only the interpreter name.
func EvaluateCommandParts(commandParts []string) Decision {
	if len(commandParts) == 0 {
		return Decision{Action: ActionAllow}
	}
	if len(commandParts) >= 3 && isShellInterpreter(commandParts[0]) && commandParts[1] == "-c" {
		return Evaluate(commandParts[2])
	}
	return Evaluate(strings.Join(commandParts, " "))
}

func isShellInterpreter(command string) bool {
	command = strings.TrimSpace(command)
	lastSlash := strings.LastIndex(command, "/")
	if lastSlash >= 0 {
		command = command[lastSlash+1:]
	}
	switch command {
	case "sh", "bash", "zsh", "dash", "ksh":
		return true
	default:
		return false
	}
}

// Evaluate classifies a shell command using non-bypassable hardline patterns.
func Evaluate(command string) Decision {
	normalizedCommand := normalize(command)
	if normalizedCommand == "" {
		return Decision{Action: ActionAllow}
	}
	for _, entry := range neverRunPatterns {
		if entry.pattern.MatchString(normalizedCommand) {
			return Decision{Action: ActionDeny, Reason: entry.reason, Risk: "critical"}
		}
	}
	for _, entry := range approvalPatterns {
		if entry.pattern.MatchString(normalizedCommand) {
			return Decision{Action: ActionRequireApproval, Reason: entry.reason, Risk: "high"}
		}
	}
	return Decision{Action: ActionAllow}
}

func normalize(command string) string {
	command = strings.ReplaceAll(command, "\\\n", " ")
	command = strings.Join(strings.Fields(command), " ")
	return command
}
