package commandpolicy

import "testing"

func TestEvaluateDeniesNeverRunCommands(t *testing.T) {
	tests := []struct {
		name    string
		command string
		reason  string
	}{
		{name: "root recursive wipe", command: "rm -rf /", reason: "root filesystem"},
		{name: "root recursive wipe reversed flags", command: "rm -fr /", reason: "root filesystem"},
		{name: "root recursive wipe uppercase flags", command: "rm -Rf /", reason: "root filesystem"},
		{name: "root recursive wipe with sudo", command: "sudo rm -rf --no-preserve-root /", reason: "root filesystem"},
		{name: "root recursive wipe with separator", command: "echo before; rm -rf /", reason: "root filesystem"},
		{name: "root contents wipe", command: "rm -rf /*", reason: "root filesystem"},
		{name: "root recursive wipe after another path", command: "rm -rf /tmp/cache /", reason: "root filesystem"},
		{name: "mkfs ext4 device", command: "mkfs.ext4 /dev/sda1", reason: "format"},
		{name: "mkfs generic device", command: "mkfs /dev/vda", reason: "format"},
		{name: "mkfs nvme partition", command: "mkfs.xfs -f /dev/nvme0n1p1", reason: "format"},
		{name: "dd zero block device", command: "dd if=/dev/zero of=/dev/nvme0n1 bs=1M", reason: "overwrite"},
		{name: "dd block device first", command: "dd of=/dev/sdb if=image.raw bs=4M", reason: "overwrite"},
		{name: "shred block device", command: "shred -n 1 /dev/mapper/root", reason: "shred"},
		{name: "fork bomb compact", command: ":(){ :|:& };:", reason: "fork bomb"},
		{name: "fork bomb spaced", command: ": ( ) { : | : & } ; :", reason: "fork bomb"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decision := Evaluate(test.command)
			if decision.Action != ActionDeny {
				t.Fatalf("Evaluate(%q) action = %q, want %q", test.command, decision.Action, ActionDeny)
			}
			if test.reason != "" && !contains(decision.Reason, test.reason) {
				t.Fatalf("Evaluate(%q) reason = %q, want containing %q", test.command, decision.Reason, test.reason)
			}
		})
	}
}

func TestEvaluateRequiresApprovalForDangerousCommands(t *testing.T) {
	tests := []struct {
		name    string
		command string
		reason  string
	}{
		{name: "recursive forced remove relative", command: "rm -rf ./build", reason: "recursive forced removal"},
		{name: "recursive forced remove reversed flags", command: "rm -fr ./build", reason: "recursive forced removal"},
		{name: "recursive forced remove absolute non-root", command: "rm -rf /tmp/build-output", reason: "recursive forced removal"},
		{name: "sudo apt", command: "sudo apt update", reason: "sudo"},
		{name: "sudo shell", command: "sudo sh -c 'echo ok'", reason: "sudo"},
		{name: "chmod 777", command: "chmod 777 /tmp/example", reason: "chmod"},
		{name: "chmod recursive", command: "chmod -R 755 /tmp/example", reason: "chmod"},
		{name: "chown recursive", command: "chown -R user:group /srv/app", reason: "ownership"},
		{name: "systemctl restart", command: "systemctl restart nginx", reason: "service"},
		{name: "service stop", command: "service nginx stop", reason: "service"},
		{name: "shutdown", command: "shutdown -h now", reason: "shutdown"},
		{name: "reboot", command: "reboot", reason: "shutdown"},
		{name: "poweroff", command: "poweroff now", reason: "shutdown"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decision := Evaluate(test.command)
			if decision.Action != ActionRequireApproval {
				t.Fatalf("Evaluate(%q) action = %q, want %q", test.command, decision.Action, ActionRequireApproval)
			}
			if test.reason != "" && !contains(decision.Reason, test.reason) {
				t.Fatalf("Evaluate(%q) reason = %q, want containing %q", test.command, decision.Reason, test.reason)
			}
		})
	}
}

func TestEvaluateAllowsRoutineCommands(t *testing.T) {
	tests := []string{
		"ls -la",
		"grep -R TODO internal",
		"go test ./...",
		"rm ./temporary-file",
		"rm -r ./empty-directory",
		"chmod 644 README.md",
		"chown user:group ./file",
		"dd if=image.raw of=./disk-image-copy.raw bs=4M",
		"mkfs.ext4 ./disk-image-file.img",
	}
	for _, command := range tests {
		t.Run(command, func(t *testing.T) {
			decision := Evaluate(command)
			if decision.Action != ActionAllow {
				t.Fatalf("Evaluate(%q) action = %q, want %q (%s)", command, decision.Action, ActionAllow, decision.Reason)
			}
		})
	}
}

func TestEvaluateNormalizesWhitespaceAndLineContinuations(t *testing.T) {
	tests := []struct {
		name       string
		command    string
		wantAction Action
	}{
		{name: "backslash newline sudo", command: "sudo \\\napt update", wantAction: ActionRequireApproval},
		{name: "extra spaces root wipe", command: "  rm    -rf    /   ", wantAction: ActionDeny},
		{name: "tabs root contents wipe", command: "rm\t-rf\t/*", wantAction: ActionDeny},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decision := Evaluate(test.command)
			if decision.Action != test.wantAction {
				t.Fatalf("Evaluate(%q) action = %q, want %q", test.command, decision.Action, test.wantAction)
			}
		})
	}
}

func TestEvaluateCommandPartsUnwrapsShellCommand(t *testing.T) {
	tests := []struct {
		name         string
		commandParts []string
		wantAction   Action
	}{
		{name: "sh deny", commandParts: []string{"sh", "-c", "rm -rf /"}, wantAction: ActionDeny},
		{name: "bash approval", commandParts: []string{"/bin/bash", "-c", "rm -rf ./build"}, wantAction: ActionRequireApproval},
		{name: "direct approval", commandParts: []string{"rm", "-rf", "./build"}, wantAction: ActionRequireApproval},
		{name: "direct allow", commandParts: []string{"echo", "hello"}, wantAction: ActionAllow},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decision := EvaluateCommandParts(test.commandParts)
			if decision.Action != test.wantAction {
				t.Fatalf("EvaluateCommandParts(%v) action = %q, want %q", test.commandParts, decision.Action, test.wantAction)
			}
		})
	}
}

func contains(text string, substring string) bool {
	return len(substring) == 0 || (len(text) >= len(substring) && containsAt(text, substring))
}

func containsAt(text string, substring string) bool {
	for index := 0; index+len(substring) <= len(text); index++ {
		if text[index:index+len(substring)] == substring {
			return true
		}
	}
	return false
}
