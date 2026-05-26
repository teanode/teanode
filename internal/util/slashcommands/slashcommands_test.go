package slashcommands

import (
	"strings"
	"testing"
)

func TestParse(test *testing.T) {
	cases := []struct {
		label         string
		message       string
		wantName      string
		wantArguments string
		wantOk        bool
	}{
		// Valid commands without arguments.
		{label: "new command", message: "/new", wantName: "new", wantOk: true},
		{label: "reset command", message: "/reset", wantName: "reset", wantOk: true},
		{label: "stop command", message: "/stop", wantName: "stop", wantOk: true},
		{label: "status command", message: "/status", wantName: "status", wantOk: true},
		{label: "help command", message: "/help", wantName: "help", wantOk: true},

		// Command with arguments.
		{label: "model without arguments", message: "/model", wantName: "model", wantOk: true},
		{label: "model with arguments", message: "/model gpt-4", wantName: "model", wantArguments: "gpt-4", wantOk: true},
		{label: "model with extra spaces", message: "/model   claude-3  ", wantName: "model", wantArguments: "claude-3", wantOk: true},

		// Case insensitivity.
		{label: "uppercase command", message: "/NEW", wantName: "new", wantOk: true},
		{label: "mixed case command", message: "/Help", wantName: "help", wantOk: true},

		// Telegram @botname suffix.
		{label: "command with bot suffix", message: "/help@mybot", wantName: "help", wantOk: true},
		{label: "command with bot suffix and arguments", message: "/model@mybot gpt-4", wantName: "model", wantArguments: "gpt-4", wantOk: true},

		// Not a command.
		{label: "plain text", message: "hello", wantOk: false},
		{label: "empty string", message: "", wantOk: false},
		{label: "slash only", message: "/", wantOk: false},
		{label: "unknown command", message: "/unknown", wantOk: false},
		{label: "text starting with slash-like", message: "/notacommand argument", wantOk: false},
	}

	for _, testCase := range cases {
		test.Run(testCase.label, func(test *testing.T) {
			name, arguments, ok := Parse(testCase.message)
			if ok != testCase.wantOk {
				test.Errorf("Parse(%q): ok = %v, want %v", testCase.message, ok, testCase.wantOk)
			}
			if name != testCase.wantName {
				test.Errorf("Parse(%q): name = %q, want %q", testCase.message, name, testCase.wantName)
			}
			if arguments != testCase.wantArguments {
				test.Errorf("Parse(%q): arguments = %q, want %q", testCase.message, arguments, testCase.wantArguments)
			}
		})
	}
}

func TestHelpText(test *testing.T) {
	helpText := HelpText()

	// Should start with the header.
	if !strings.HasPrefix(helpText, "Available commands:\n") {
		test.Errorf("HelpText() should start with 'Available commands:\\n', got %q", helpText[:40])
	}

	// Should contain all registered commands.
	expectedCommands := []string{"/new", "/reset", "/stop", "/model", "/status", "/help"}
	for _, expected := range expectedCommands {
		if !strings.Contains(helpText, expected) {
			test.Errorf("HelpText() missing command %q", expected)
		}
	}

	// Model command should show its arguments placeholder.
	if !strings.Contains(helpText, "/model [name]") {
		test.Error("HelpText() should show arguments for /model command")
	}
}
