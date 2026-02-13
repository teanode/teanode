package command

import (
	"fmt"
	"strings"
)

type definition struct {
	Name        string
	Args        string
	Description string
}

var commands = []definition{
	{Name: "new", Description: "Start a new session"},
	{Name: "reset", Description: "Clear current session history"},
	{Name: "stop", Description: "Cancel the current run"},
	{Name: "model", Args: "[name]", Description: "Show or set the model"},
	{Name: "status", Description: "Show bot status"},
	{Name: "help", Description: "Show available commands"},
}

// Parse checks if a message is a slash command.
// Returns (name, args, true) or ("", "", false).
func Parse(message string) (name, arguments string, ok bool) {
	if !strings.HasPrefix(message, "/") {
		return "", "", false
	}

	// Split into command and args.
	parts := strings.SplitN(message[1:], " ", 2)
	if len(parts) == 0 || parts[0] == "" {
		return "", "", false
	}

	// Strip @botname suffix (Telegram sends /cmd@botname).
	commandName := strings.ToLower(parts[0])
	if atIndex := strings.Index(commandName, "@"); atIndex > 0 {
		commandName = commandName[:atIndex]
	}

	// Check if it's a known command.
	for _, commandDef := range commands {
		if commandDef.Name == commandName {
			if len(parts) > 1 {
				return commandName, strings.TrimSpace(parts[1]), true
			}
			return commandName, "", true
		}
	}
	return "", "", false
}

// HelpText returns a formatted help string listing all commands.
func HelpText() string {
	var builder strings.Builder
	builder.WriteString("Available commands:\n")
	for _, commandDef := range commands {
		if commandDef.Args != "" {
			fmt.Fprintf(&builder, "  /%s %s — %s\n", commandDef.Name, commandDef.Args, commandDef.Description)
		} else {
			fmt.Fprintf(&builder, "  /%s — %s\n", commandDef.Name, commandDef.Description)
		}
	}
	return builder.String()
}
