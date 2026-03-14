package v1api

import (
	"context"
	"encoding/json"
	"sort"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/tools"
)

// toolPolicyGroupEntry is one group within a tool in the toolPolicies.list response.
type toolPolicyGroupEntry struct {
	Group         models.ToolPolicyGroup `json:"group"`
	DefaultPolicy models.ToolPolicyLevel `json:"defaultPolicy"`
}

// toolPolicyEntry is one tool in the toolPolicies.list response.
type toolPolicyEntry struct {
	Name   string                 `json:"name"`
	Groups []toolPolicyGroupEntry `json:"groups"`
	Source string                 `json:"source"` // "builtin" or "skill"
	Skill  string                 `json:"skill,omitempty"`
}

// loadAllToolActionGroups returns the action groups for all tools: builtin + skill-contributed.
// It also returns a validGroups map used for validation during updates.
func (self *webSocketConnection) loadAllToolActionGroups() ([]toolPolicyEntry, map[string][]string, error) {
	// 1. Builtin tools.
	registry := tools.NewToolRegistry()
	builtinGroupInfos := registry.ToolActionGroups()

	entries := make([]toolPolicyEntry, 0)
	validGroups := make(map[string][]string)

	builtinNames := registry.Names() // sorted
	for _, name := range builtinNames {
		infos := builtinGroupInfos[name]
		groupEntries := make([]toolPolicyGroupEntry, 0, len(infos))
		groupStrings := make([]string, 0, len(infos))
		for _, info := range infos {
			groupEntries = append(groupEntries, toolPolicyGroupEntry{
				Group:         info.Group,
				DefaultPolicy: info.Default,
			})
			groupStrings = append(groupStrings, string(info.Group))
		}
		entries = append(entries, toolPolicyEntry{Name: name, Groups: groupEntries, Source: "builtin"})
		validGroups[name] = groupStrings
	}

	// 2. Skill-contributed tools.
	var skills []*models.Skill
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, tx store.Transaction) error {
		var listErr error
		skills, listErr = tx.ListSkills(ctx, nil)
		return listErr
	}); err != nil {
		return nil, nil, err
	}

	var skillEntries []toolPolicyEntry
	for _, skill := range skills {
		skillName := skill.GetName()
		if skill.Tools == nil {
			continue
		}
		for _, tool := range *skill.Tools {
			if tool.Name == "" {
				continue
			}
			// Skip if a builtin tool already has this name.
			if _, exists := validGroups[tool.Name]; exists {
				continue
			}
			groupStrings := []string{"*"}
			skillEntries = append(skillEntries, toolPolicyEntry{
				Name: tool.Name,
				Groups: []toolPolicyGroupEntry{
					{Group: models.ToolPolicyGroupAll, DefaultPolicy: models.ToolPolicyAnyone},
				},
				Source: "skill",
				Skill:  skillName,
			})
			validGroups[tool.Name] = groupStrings
		}
	}

	// Sort skill entries by name and append.
	sort.Slice(skillEntries, func(i, j int) bool {
		return skillEntries[i].Name < skillEntries[j].Name
	})
	entries = append(entries, skillEntries...)

	return entries, validGroups, nil
}

// --- toolPolicies.list ---

func (self *webSocketConnection) handleToolPoliciesList(frame requestFrame) (interface{}, error) {
	if err := self.requireAdmin(); err != nil {
		return nil, err
	}

	toolEntries, _, err := self.loadAllToolActionGroups()
	if err != nil {
		return nil, rpcError(500, "loading tools: "+err.Error())
	}

	// Load current configured policies.
	var policies []*models.ToolPolicyConfiguration
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		configuration, loadErr := transaction.GetConfiguration(ctx, nil)
		if loadErr != nil {
			return loadErr
		}
		if configuration != nil && configuration.ToolPolicies != nil {
			policies = *configuration.ToolPolicies
		}
		return nil
	}); err != nil {
		return nil, rpcError(500, "loading config: "+err.Error())
	}
	if policies == nil {
		policies = make([]*models.ToolPolicyConfiguration, 0)
	}

	return map[string]interface{}{
		"tools":    toolEntries,
		"policies": policies,
	}, nil
}

// --- toolPolicies.update ---

func (self *webSocketConnection) handleToolPoliciesUpdate(frame requestFrame) (interface{}, error) {
	if err := self.requireAdmin(); err != nil {
		return nil, err
	}

	var parameters struct {
		Policies []*models.ToolPolicyConfiguration `json:"policies"`
	}
	if frame.Params != nil {
		if err := json.Unmarshal(frame.Params, &parameters); err != nil {
			return nil, rpcError(400, "invalid parameters: "+err.Error())
		}
	}

	// Load all known tool names (builtin + skill) for validation.
	_, validGroups, err := self.loadAllToolActionGroups()
	if err != nil {
		return nil, rpcError(500, "loading tools: "+err.Error())
	}

	validLevels := map[models.ToolPolicyLevel]bool{
		models.ToolPolicyDisabled:       true,
		models.ToolPolicyAdminApproval:  true,
		models.ToolPolicyAdminOnly:      true,
		models.ToolPolicyAnyoneApproval: true,
		models.ToolPolicyAnyone:         true,
	}

	for _, entry := range parameters.Policies {
		toolName := entry.GetTool()
		group := entry.GetGroup()
		level := entry.GetLevel()

		if toolName == "" || group == "" || level == "" {
			return nil, rpcError(400, "each policy entry must have tool, group, and level")
		}
		groups, exists := validGroups[toolName]
		if !exists {
			return nil, rpcError(400, "unknown tool: "+toolName)
		}
		groupString := string(group)
		validGroup := groupString == "*"
		for _, knownGroup := range groups {
			if knownGroup == groupString {
				validGroup = true
				break
			}
		}
		if !validGroup {
			return nil, rpcError(400, "invalid group '"+groupString+"' for tool '"+toolName+"'")
		}
		if !validLevels[level] {
			return nil, rpcError(400, "invalid policy level: "+string(level))
		}
	}

	// Save to configuration (replaces the entire ToolPolicies slice).
	policiesToSave := parameters.Policies
	if saveErr := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		_, modifyError := transaction.ModifyConfiguration(ctx, func(configuration *models.Configuration) error {
			configuration.ToolPolicies = &policiesToSave
			return nil
		}, nil)
		return modifyError
	}); saveErr != nil {
		return nil, rpcError(500, "saving config: "+saveErr.Error())
	}

	return map[string]interface{}{"ok": true}, nil
}
