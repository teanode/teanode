package voice

import (
	"context"

	"github.com/teanode/teanode/internal/coordinators"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/runners"
)

// Dispatcher is the minimal coordinator surface the voice session depends on.
// *coordinators.Coordinator satisfies this interface directly — no adapter needed.
type Dispatcher interface {
	Run(ctx context.Context, parameters coordinators.RunParameters, callerCallbacks *runners.RunCallbacks) (*coordinators.RunHandle, error)
	AbortRun(runId string) bool
	ProviderRegistry() *providers.ProviderRegistry
}
