package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/teanode/teanode/internal/jobs"
	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/security"
	"github.com/teanode/teanode/internal/web"
)

func (self *api) handleJobWebhook(writer http.ResponseWriter, request *http.Request) error {
	if request.Method == http.MethodGet || request.Method == http.MethodHead {
		return web.Error(405, "method not allowed")
	}

	jobID := mux.Vars(request)["id"]
	if jobID == "" {
		return web.Error(400, "missing job id")
	}

	var jobModel *models.Job
	if err := store.StoreFromContext(request.Context()).Transaction(request.Context(), func(ctx context.Context, transaction store.Transaction) error {
		var getError error
		jobModel, getError = transaction.GetJob(ctx, jobID, nil)
		return getError
	}); err != nil {
		if err == store.ErrNotFound {
			return web.ErrNotFound
		}
		return web.Error(500, "loading job: "+err.Error())
	}

	if jobModel.GetTrigger() != models.JobTriggerKindWebhook {
		return web.Error(404, "job webhook not found")
	}
	if !jobModel.GetEnabled() {
		return web.Error(409, "job is disabled")
	}

	providedSecret := request.Header.Get("X-TeaNode-Webhook-Secret")
	if providedSecret == "" {
		providedSecret = request.URL.Query().Get("secret")
	}
	if jobModel.GetWebhookSecret() == "" || subtle.ConstantTimeCompare([]byte(providedSecret), []byte(jobModel.GetWebhookSecret())) != 1 {
		return web.ErrUnauthorized
	}

	jobRun, err := jobs.SchedulerFromContext(request.Context()).TriggerJobWithMetadata(request.Context(), jobID, jobs.TriggerMetadata{
		Trigger:       models.JobTriggerKindWebhook,
		RequestMethod: request.Method,
		RequestPath:   request.URL.Path,
		RemoteAddress: request.RemoteAddr,
	})
	if err != nil {
		return web.Error(500, "triggering webhook job: "+err.Error())
	}

	writer.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(writer).Encode(map[string]interface{}{
		"status":   "accepted",
		"jobRunId": jobRun.ID,
	})
}

func generateWebhookSecret() string {
	return security.GenerateRandomString(32, security.LowerAlphaNumeric)
}
