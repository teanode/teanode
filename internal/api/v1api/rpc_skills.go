package v1api

import (
	"context"
	"encoding/json"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/skills"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/ptrto"
)

// handleSkillsLocalList returns an empty list (no local skills with hardcoded library).
func (self *webSocketConnection) handleSkillsLocalList(frame requestFrame) {
	if !self.requireAdmin(frame) {
		return
	}
	self.sendResponse(frame.ID, map[string]interface{}{
		"skills": []interface{}{},
	})
}

// handleSkillsLibrarySearch searches the official skill library.
func (self *webSocketConnection) handleSkillsLibrarySearch(frame requestFrame) {
	if !self.requireAdmin(frame) {
		return
	}
	var parameters struct {
		Query string `json:"query"`
	}
	if frame.Params != nil {
		json.Unmarshal(frame.Params, &parameters)
	}
	results, err := skills.Search(self.ctx, parameters.Query)
	if err != nil {
		self.sendError(frame.ID, 500, "searching skills: "+err.Error())
		return
	}
	self.sendResponse(frame.ID, map[string]interface{}{
		"results": results,
	})
}

// handleSkillsInstall installs a skill from the official library.
func (self *webSocketConnection) handleSkillsInstall(frame requestFrame) {
	if !self.requireAdmin(frame) {
		return
	}
	var parameters struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	}
	if frame.Params != nil {
		json.Unmarshal(frame.Params, &parameters)
	}
	if parameters.Name == "" {
		self.sendError(frame.ID, 400, "name is required")
		return
	}
	info, err := skills.Install(self.ctx, parameters.Name, parameters.Version)
	if err != nil {
		self.sendError(frame.ID, 500, "installing skill: "+err.Error())
		return
	}
	self.sendResponse(frame.ID, map[string]interface{}{
		"skill": info,
	})
}

// handleSkillsInstalledList returns all installed skills.
func (self *webSocketConnection) handleSkillsInstalledList(frame requestFrame) {
	if !self.requireAdmin(frame) {
		return
	}
	var installed []*models.Skill
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, tx store.Transaction) error {
		var err error
		installed, err = tx.ListSkills(ctx, nil)
		return err
	}); err != nil {
		self.sendError(frame.ID, 500, "listing skills: "+err.Error())
		return
	}
	type secretDeclarationResponse struct {
		Key         string `json:"key"`
		Description string `json:"description,omitempty"`
	}
	type installedSkillResponse struct {
		Name        string                      `json:"name"`
		Description string                      `json:"description,omitempty"`
		Version     string                      `json:"version"`
		Enabled     bool                        `json:"enabled"`
		SourceID    string                      `json:"sourceId,omitempty"`
		Publisher   string                      `json:"publisher,omitempty"`
		Secrets     []secretDeclarationResponse `json:"secrets,omitempty"`
	}
	result := make([]installedSkillResponse, 0, len(installed))
	for _, skill := range installed {
		response := installedSkillResponse{
			Name:        skill.GetName(),
			Description: skill.GetDescription(),
			Version:     skill.GetVersion(),
			Enabled:     skill.GetEnabled(),
			SourceID:    skill.GetSource(),
			Publisher:   skill.GetPublisher(),
		}
		for _, secret := range skill.GetSecrets() {
			response.Secrets = append(response.Secrets, secretDeclarationResponse{
				Key:         secret.Key,
				Description: secret.Description,
			})
		}
		result = append(result, response)
	}
	self.sendResponse(frame.ID, map[string]interface{}{
		"skills": result,
	})
}

// handleSkillsUninstall removes an installed skill.
func (self *webSocketConnection) handleSkillsUninstall(frame requestFrame) {
	if !self.requireAdmin(frame) {
		return
	}
	var parameters struct {
		Name string `json:"name"`
	}
	if frame.Params != nil {
		json.Unmarshal(frame.Params, &parameters)
	}
	if parameters.Name == "" {
		self.sendError(frame.ID, 400, "name is required")
		return
	}
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, tx store.Transaction) error {
		return tx.DeleteSkill(ctx, parameters.Name, nil)
	}); err != nil {
		self.sendError(frame.ID, 500, "uninstalling skill: "+err.Error())
		return
	}
	self.sendResponse(frame.ID, map[string]interface{}{
		"ok": true,
	})
}

// handleSkillsUpdate checks for and applies updates to installed skills.
func (self *webSocketConnection) handleSkillsUpdate(frame requestFrame) {
	if !self.requireAdmin(frame) {
		return
	}
	var parameters struct {
		Name string `json:"name"`
	}
	if frame.Params != nil {
		json.Unmarshal(frame.Params, &parameters)
	}
	updated, err := skills.Update(self.ctx, parameters.Name)
	if err != nil {
		self.sendError(frame.ID, 500, "updating skills: "+err.Error())
		return
	}
	self.sendResponse(frame.ID, map[string]interface{}{
		"updated": updated,
	})
}

// handleSkillsSetEnabled toggles a skill's enabled state.
func (self *webSocketConnection) handleSkillsSetEnabled(frame requestFrame) {
	if !self.requireAdmin(frame) {
		return
	}
	var parameters struct {
		Name    string `json:"name"`
		Enabled *bool  `json:"enabled"`
	}
	if frame.Params != nil {
		json.Unmarshal(frame.Params, &parameters)
	}
	if parameters.Name == "" {
		self.sendError(frame.ID, 400, "name is required")
		return
	}
	if parameters.Enabled == nil {
		self.sendError(frame.ID, 400, "enabled is required")
		return
	}
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, tx store.Transaction) error {
		_, err := tx.ModifySkill(ctx, parameters.Name, func(skill *models.Skill) error {
			skill.Enabled = ptrto.Value(*parameters.Enabled)
			return nil
		}, nil)
		return err
	}); err != nil {
		self.sendError(frame.ID, 500, "setting skill enabled: "+err.Error())
		return
	}
	self.sendResponse(frame.ID, map[string]interface{}{
		"ok": true,
	})
}

// handleSecretsList returns all secret declarations from installed skills,
// with a boolean indicating whether each secret has a value configured.
func (self *webSocketConnection) handleSecretsList(frame requestFrame) {
	if !self.requireAdmin(frame) {
		return
	}

	// Load installed skills.
	var installed []*models.Skill
	var configuration *models.Configuration
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, tx store.Transaction) error {
		var err error
		installed, err = tx.ListSkills(ctx, nil)
		if err != nil {
			return err
		}
		configuration, err = tx.GetConfiguration(ctx, nil)
		return err
	}); err != nil {
		self.sendError(frame.ID, 500, "listing secrets: "+err.Error())
		return
	}

	// Build set of configured secret keys.
	configuredKeys := map[string]bool{}
	if configuration != nil {
		for _, secret := range configuration.GetSecrets() {
			if secret.Key != nil && secret.Value != nil && *secret.Value != "" {
				configuredKeys[*secret.Key] = true
			}
		}
	}

	// Collect secret declarations, deduplicating by key.
	type secretInfo struct {
		Key         string   `json:"key"`
		Description string   `json:"description,omitempty"`
		Skills      []string `json:"skills"`
		Configured  bool     `json:"configured"`
	}
	secretMap := map[string]*secretInfo{}
	var secretOrder []string
	for _, skill := range installed {
		if !skill.GetEnabled() {
			continue
		}
		for _, secret := range skill.GetSecrets() {
			if existing, ok := secretMap[secret.Key]; ok {
				existing.Skills = append(existing.Skills, skill.GetName())
			} else {
				secretMap[secret.Key] = &secretInfo{
					Key:         secret.Key,
					Description: secret.Description,
					Skills:      []string{skill.GetName()},
					Configured:  configuredKeys[secret.Key],
				}
				secretOrder = append(secretOrder, secret.Key)
			}
		}
	}

	secrets := make([]secretInfo, 0, len(secretOrder))
	for _, key := range secretOrder {
		secrets = append(secrets, *secretMap[key])
	}

	self.sendResponse(frame.ID, map[string]interface{}{
		"secrets": secrets,
	})
}

// handleSecretsSet sets or clears a secret value in the configuration.
func (self *webSocketConnection) handleSecretsSet(frame requestFrame) {
	if !self.requireAdmin(frame) {
		return
	}
	var parameters struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	if frame.Params != nil {
		json.Unmarshal(frame.Params, &parameters)
	}
	if parameters.Key == "" {
		self.sendError(frame.ID, 400, "key is required")
		return
	}

	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, tx store.Transaction) error {
		_, err := tx.ModifyConfiguration(ctx, func(configuration *models.Configuration) error {
			var secrets []*models.SecretConfiguration
			if configuration.Secrets != nil {
				secrets = *configuration.Secrets
			}

			// Remove existing entry for this key.
			filtered := make([]*models.SecretConfiguration, 0, len(secrets))
			for _, s := range secrets {
				if s.Key != nil && *s.Key == parameters.Key {
					continue
				}
				filtered = append(filtered, s)
			}

			// If value is non-empty, add new entry.
			if parameters.Value != "" {
				filtered = append(filtered, &models.SecretConfiguration{
					Key:   ptrto.Value(parameters.Key),
					Value: ptrto.Value(parameters.Value),
				})
			}

			configuration.Secrets = &filtered
			return nil
		}, nil)
		return err
	}); err != nil {
		self.sendError(frame.ID, 500, "setting secret: "+err.Error())
		return
	}
	self.sendResponse(frame.ID, map[string]interface{}{
		"ok": true,
	})
}
