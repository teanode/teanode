package filesystem

import (
	"context"
	"testing"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/tools"
)

func adminCtx() context.Context {
	admin := true
	user := &models.User{ID: "u1", Admin: &admin}
	return models.ContextWithUserSessionToken(context.Background(), user, nil, nil)
}

func nonAdminCtx() context.Context {
	admin := false
	user := &models.User{ID: "u2", Admin: &admin}
	return models.ContextWithUserSessionToken(context.Background(), user, nil, nil)
}

func TestPolicy_NonAdminReadDenied(t *testing.T) {
	tool := &filesystemTool{}
	for _, action := range []string{"read", "list", "info", "search"} {
		decision := tools.ResolveToolPolicy(nonAdminCtx(), tool, "filesystem", `{"action":"`+action+`"}`)
		if decision.Action != tools.PolicyDeny {
			t.Errorf("action %q: expected PolicyDeny for non-admin, got %q", action, decision.Action)
		}
	}
}

func TestPolicy_AdminReadAllowed(t *testing.T) {
	tool := &filesystemTool{}
	for _, action := range []string{"read", "list", "info", "search"} {
		decision := tools.ResolveToolPolicy(adminCtx(), tool, "filesystem", `{"action":"`+action+`"}`)
		if decision.Action != tools.PolicyAllow {
			t.Errorf("action %q: expected PolicyAllow for admin, got %q", action, decision.Action)
		}
	}
}

func TestPolicy_NonAdminWriteDenied(t *testing.T) {
	tool := &filesystemTool{}
	for _, action := range []string{"write", "mkdir", "move", "delete"} {
		decision := tools.ResolveToolPolicy(nonAdminCtx(), tool, "filesystem", `{"action":"`+action+`"}`)
		if decision.Action != tools.PolicyDeny {
			t.Errorf("action %q: expected PolicyDeny for non-admin, got %q", action, decision.Action)
		}
	}
}

func TestPolicy_AdminWriteRequiresApproval(t *testing.T) {
	tool := &filesystemTool{}
	for _, action := range []string{"write", "mkdir", "move", "delete"} {
		decision := tools.ResolveToolPolicy(adminCtx(), tool, "filesystem", `{"action":"`+action+`"}`)
		if decision.Action != tools.PolicyRequireApproval {
			t.Errorf("action %q: expected PolicyRequireApproval for admin, got %q", action, decision.Action)
		}
		if decision.Risk != "high" {
			t.Errorf("action %q: expected risk=high, got %q", action, decision.Risk)
		}
	}
}

func TestPolicy_AdminSearchAllowed(t *testing.T) {
	tool := &filesystemTool{}
	decision := tools.ResolveToolPolicy(adminCtx(), tool, "filesystem", `{"action":"search"}`)
	if decision.Action != tools.PolicyAllow {
		t.Errorf("expected PolicyAllow for admin search, got %q", decision.Action)
	}
}
