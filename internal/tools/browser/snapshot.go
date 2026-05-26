package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/teanode/teanode/internal/integrations/browsers"
)

// accessibilityValue represents a typed value in the accessibility tree.
type accessibilityValue struct {
	Type  string      `json:"type"`
	Value interface{} `json:"value"`
}

// accessibilityProperty represents a named property on an accessibility node.
type accessibilityProperty struct {
	Name  string             `json:"name"`
	Value accessibilityValue `json:"value"`
}

// accessibilityNodeExt is an accessibility tree node with the backendDOMNodeId
// field needed for reference-based interactions (used by the AX fallback path).
type accessibilityNodeExt struct {
	NodeID           string                  `json:"nodeId"`
	ParentID         string                  `json:"parentId"`
	BackendDOMNodeID int                     `json:"backendDOMNodeId"`
	Role             accessibilityValue      `json:"role"`
	Name             accessibilityValue      `json:"name"`
	Value            *accessibilityValue     `json:"value"`
	Properties       []accessibilityProperty `json:"properties"`
	ChildIDs         []string                `json:"childIds"`
	Ignored          bool                    `json:"ignored"`
}

// snapshotResult holds the output of an enhanced snapshot.
type snapshotResult struct {
	Tree     string `json:"tree"`
	RefCount int    `json:"refCount"`
	PageURL  string `json:"pageUrl,omitempty"`
	Title    string `json:"title,omitempty"`
}

// domSnapshotResponse is the structure returned by the DOM walker JavaScript.
type domSnapshotResponse struct {
	Tree     string                 `json:"tree"`
	RefCount int                    `json:"refCount"`
	Refs     []domReferenceMetadata `json:"refs"`
	PageURL  string                 `json:"pageUrl"`
	Title    string                 `json:"title"`
}

// domReferenceMetadata holds the role and name for a single DOM-based reference.
type domReferenceMetadata struct {
	Role string `json:"role"`
	Name string `json:"name"`
}

// executeEnhancedSnapshot performs a DOM-based snapshot that assigns stable
// integer refs to interactive elements. The refs are stored both in the
// browser's window.__teanodeRefs array (for DOM resolution) and in
// globalReferenceStore (for metadata like role and name).
func executeEnhancedSnapshot(ctx context.Context, browser browsers.Browser, connectionId string) (string, error) {
	sessionId, err := resolveSessionId(ctx, browser, connectionId)
	if err != nil {
		return "", err
	}

	result, err := browser.SendCDPCommand(ctx, "Runtime.evaluate", map[string]interface{}{
		"expression":    domSnapshotScript,
		"returnByValue": true,
	}, sessionId)
	if err != nil {
		return "", fmt.Errorf("browser: DOM snapshot evaluation: %w", err)
	}

	var evalResponse struct {
		Result struct {
			Value json.RawMessage `json:"value"`
		} `json:"result"`
		ExceptionDetails *struct {
			Text string `json:"text"`
		} `json:"exceptionDetails"`
	}
	if err := json.Unmarshal(result, &evalResponse); err != nil {
		return "", fmt.Errorf("browser: parsing DOM snapshot response: %w", err)
	}
	if evalResponse.ExceptionDetails != nil {
		return "", fmt.Errorf("browser: DOM snapshot error: %s", evalResponse.ExceptionDetails.Text)
	}

	var snapshot domSnapshotResponse
	if err := json.Unmarshal(evalResponse.Result.Value, &snapshot); err != nil {
		return "", fmt.Errorf("browser: parsing DOM snapshot value: %w", err)
	}

	// Populate the server-side reference store with metadata from the DOM walker.
	// The actual element references live in window.__teanodeRefs in the browser.
	refs := make(map[int]referenceEntry, len(snapshot.Refs))
	for index, metadata := range snapshot.Refs {
		refs[index+1] = referenceEntry{
			Role: metadata.Role,
			Name: metadata.Name,
		}
	}
	globalReferenceStore.store(sessionId, refs)

	output := snapshotResult{
		Tree:     snapshot.Tree,
		RefCount: snapshot.RefCount,
		PageURL:  snapshot.PageURL,
		Title:    snapshot.Title,
	}
	data, _ := json.Marshal(output)
	return string(data), nil
}

// buildAxTreeWithRefs builds a text accessibility tree with [reference=N] markers on
// interactive elements from CDP Accessibility.getFullAXTree nodes. This is the
// AX-based tree builder, kept as a tested utility. The primary snapshot path
// now uses the DOM-based approach (executeEnhancedSnapshot) which is more
// reliable in headless Chrome environments.
func buildAxTreeWithRefs(nodes []accessibilityNodeExt) (string, map[int]referenceEntry) {
	if len(nodes) == 0 {
		return "(empty accessibility tree)", nil
	}

	nodesById := make(map[string]*accessibilityNodeExt, len(nodes))
	for index := range nodes {
		nodesById[nodes[index].NodeID] = &nodes[index]
	}

	refs := make(map[int]referenceEntry)
	nextReference := 1

	var builder strings.Builder
	var walk func(id string, depth int)
	walk = func(id string, depth int) {
		node, ok := nodesById[id]
		if !ok || node.Ignored {
			return
		}

		role := fmt.Sprintf("%v", node.Role.Value)
		name := fmt.Sprintf("%v", node.Name.Value)

		// Skip generic/none roles without meaningful names.
		if (role == "none" || role == "generic" || role == "") && name == "" {
			for _, childId := range node.ChildIDs {
				walk(childId, depth)
			}
			return
		}

		indent := strings.Repeat("  ", depth)

		// Assign a reference to interactive elements that have a backendDOMNodeId.
		var referenceMarker string
		if isInteractiveRole(role) && node.BackendDOMNodeID != 0 {
			reference := nextReference
			nextReference++
			refs[reference] = referenceEntry{
				BackendDOMNodeID: node.BackendDOMNodeID,
				Role:             role,
				Name:             name,
			}
			referenceMarker = fmt.Sprintf("[reference=%d] ", reference)
		}

		line := indent + referenceMarker + role
		if name != "" {
			line += fmt.Sprintf(" %q", name)
		}

		// Add notable properties.
		for _, property := range node.Properties {
			switch property.Name {
			case "level":
				line += fmt.Sprintf(" (level %v)", property.Value.Value)
			case "checked":
				line += fmt.Sprintf(" checked=%v", property.Value.Value)
			case "disabled":
				if property.Value.Value == true {
					line += " disabled"
				}
			case "required":
				if property.Value.Value == true {
					line += " required"
				}
			case "expanded":
				line += fmt.Sprintf(" expanded=%v", property.Value.Value)
			case "selected":
				if property.Value.Value == true {
					line += " selected"
				}
			}
		}
		if node.Value != nil && node.Value.Value != nil {
			line += fmt.Sprintf(" value=%q", fmt.Sprintf("%v", node.Value.Value))
		}

		builder.WriteString(line)
		builder.WriteByte('\n')

		for _, childId := range node.ChildIDs {
			walk(childId, depth+1)
		}
	}

	walk(nodes[0].NodeID, 0)
	return strings.TrimRight(builder.String(), "\n"), refs
}
