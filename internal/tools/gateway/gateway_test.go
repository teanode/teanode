package gateway

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

func anonymousCtx() context.Context {
	return context.Background()
}

func TestGatewayPolicy_AdminRequiresApproval(t *testing.T) {
	tool := &gatewayTool{}
	decision := tools.ResolveToolPolicy(adminCtx(), tool, "gateway", `{"action":"restart"}`)
	if decision.Action != tools.PolicyRequireApproval {
		t.Errorf("expected PolicyRequireApproval for admin, got %q", decision.Action)
	}
	if decision.Risk != "high" {
		t.Errorf("expected risk=high, got %q", decision.Risk)
	}
}

func TestGatewayPolicy_NonAdminDenied(t *testing.T) {
	tool := &gatewayTool{}
	decision := tools.ResolveToolPolicy(nonAdminCtx(), tool, "gateway", `{"action":"restart"}`)
	if decision.Action != tools.PolicyDeny {
		t.Errorf("expected PolicyDeny for non-admin, got %q", decision.Action)
	}
}

func TestGatewayPolicy_AnonymousDenied(t *testing.T) {
	tool := &gatewayTool{}
	decision := tools.ResolveToolPolicy(anonymousCtx(), tool, "gateway", `{"action":"terminate"}`)
	if decision.Action != tools.PolicyDeny {
		t.Errorf("expected PolicyDeny for anonymous, got %q", decision.Action)
	}
}
