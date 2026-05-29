package shell

import (
	"context"
	"strings"
	"testing"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/tools"
)

func shellAdminContext() context.Context {
	admin := true
	user := &models.User{ID: "admin", Admin: &admin}
	return models.ContextWithUserSessionToken(context.Background(), user, nil, nil)
}

func TestShellArgumentPolicyDeniesNeverRunCommands(t *testing.T) {
	tool := &shellTool{}
	decision := tools.ResolveToolPolicy(shellAdminContext(), tool, "shell", `{"command":"rm -rf /"}`)
	if decision.Action != tools.PolicyDeny {
		t.Fatalf("action = %q, want %q", decision.Action, tools.PolicyDeny)
	}
	if !strings.Contains(decision.Reason, "root filesystem") {
		t.Fatalf("reason = %q, want root filesystem", decision.Reason)
	}
}

func TestShellArgumentPolicyRequiresApprovalForDangerousCommands(t *testing.T) {
	tool := &shellTool{}
	decision := tools.ResolveToolPolicy(shellAdminContext(), tool, "shell", `{"command":"rm -rf ./build"}`)
	if decision.Action != tools.PolicyRequireApproval {
		t.Fatalf("action = %q, want %q", decision.Action, tools.PolicyRequireApproval)
	}
	if decision.Risk != "high" {
		t.Fatalf("risk = %q, want high", decision.Risk)
	}
}

func TestShellExecuteBlocksNeverRunCommandEvenIfCalledDirectly(t *testing.T) {
	tool := &shellTool{}
	_, err := tool.Execute(shellAdminContext(), `{"command":"rm -rf /"}`)
	if err == nil || !strings.Contains(err.Error(), "command blocked") {
		t.Fatalf("expected command blocked error, got %v", err)
	}
}
