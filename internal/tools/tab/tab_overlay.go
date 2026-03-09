package tab

import (
	"context"
	"fmt"
	"strings"

	"github.com/teanode/teanode/internal/integrations/tabs"
	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/runners"
)

// BuildOverlay implements tools.OverlayBuilder. It returns a formatted reminder
// when a browser tab is attached to the current conversation.
func (self *tabTool) BuildOverlay(ctx context.Context) (string, error) {
	runner := runners.RunnerFromContext(ctx)
	if runner == nil {
		return "", nil
	}
	return buildTabOverlay(ctx, runner.AgentID, runner.ConversationID), nil
}

// buildTabOverlay returns a formatted reminder when a browser tab is attached
// to the current conversation. Best-effort: returns "" if no tab is attached.
func buildTabOverlay(ctx context.Context, agentID, conversationID string) string {
	broker := tabs.TabBrokerFromContext(ctx)
	if broker == nil {
		return ""
	}

	user := models.UserFromContext(ctx)
	if user == nil || user.ID == "" {
		return ""
	}

	attachment := broker.GetAttachment(user.ID, agentID, conversationID)
	if attachment == nil {
		return ""
	}

	var builder strings.Builder
	builder.WriteString("<attached-tab>\n")
	fmt.Fprintf(&builder, "A browser tab is currently attached to this conversation.\n")
	fmt.Fprintf(&builder, "Title: %s\n", attachment.TabTitle)
	fmt.Fprintf(&builder, "URL: %s\n", attachment.TabURL)
	builder.WriteString("\nYou can use tab tools to interact with the attached page.\n")
	builder.WriteString("</attached-tab>")

	return builder.String()
}
