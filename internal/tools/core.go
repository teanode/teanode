package tools

// DefaultCoreTools lists the tools whose full definitions stay in every chat
// request when the rest of the tool set is deferred for a small context
// window. The set is deliberately small: general-purpose tools with no
// external service behind them, plus the user-scoped memory and workspace
// that the agent needs to answer recall questions without a lookup round.
// Everything else is reachable through the deferred tool catalog.
//
// Override this with the tools.coreTools configuration setting.
var DefaultCoreTools = []string{
	"ask_user_question",
	"datetime",
	"filesystem",
	"shell",
	"user_memory",
	"user_workspace",
	"web_fetch",
	"web_search",
}
