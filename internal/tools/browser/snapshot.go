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
// field needed for ref-based interactions.
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

// executeEnhancedSnapshot performs an accessibility snapshot that assigns stable
// integer refs to interactive elements. The refs are stored in globalRefStore
// so that subsequent ref-based actions (click_ref, type_ref, etc.) can resolve
// them to DOM nodes.
func executeEnhancedSnapshot(ctx context.Context, browser browsers.Browser, connectionId string) (string, error) {
	sessionId, err := resolveSessionId(ctx, browser, connectionId)
	if err != nil {
		return "", err
	}

	// Enable the DOM and Accessibility domains so Chrome populates the full
	// accessibility tree (including backendDOMNodeId on each node). Without
	// these, getFullAXTree returns only the root web area.
	if _, err := browser.SendCDPCommand(ctx, "DOM.enable", nil, sessionId); err != nil {
		return "", fmt.Errorf("enabling DOM domain: %w", err)
	}
	// Force Chrome to compute the full DOM tree by requesting the document
	// root with depth=-1. DOM.enable starts event monitoring but does not
	// build the tree; without this call getFullAXTree returns only
	// RootWebArea. The depth=-1 parameter is critical: the default depth
	// only materializes a few levels, which means deeper DOM nodes lack a
	// backendDOMNodeId in the accessibility tree.
	if _, err := browser.SendCDPCommand(ctx, "DOM.getDocument", map[string]interface{}{
		"depth": -1,
	}, sessionId); err != nil {
		return "", fmt.Errorf("getting DOM document: %w", err)
	}
	if _, err := browser.SendCDPCommand(ctx, "Accessibility.enable", nil, sessionId); err != nil {
		return "", fmt.Errorf("enabling Accessibility domain: %w", err)
	}

	// depth=-1 retrieves the complete accessibility tree. Without this
	// parameter Chrome defaults to a shallow depth (typically 2 levels)
	// which often returns only the RootWebArea node.
	result, err := browser.SendCDPCommand(ctx, "Accessibility.getFullAXTree", map[string]interface{}{
		"depth": -1,
	}, sessionId)
	if err != nil {
		return "", err
	}

	var response struct {
		Nodes []accessibilityNodeExt `json:"nodes"`
	}
	if err := json.Unmarshal(result, &response); err != nil {
		return "", fmt.Errorf("parsing accessibility tree: %w", err)
	}

	tree, refs := buildAXTreeWithRefs(response.Nodes)
	globalRefStore.store(sessionId, refs)

	// Try to get page metadata for context.
	pageURL, title := getPageMetadata(ctx, browser, sessionId)

	output := snapshotResult{
		Tree:     tree,
		RefCount: len(refs),
		PageURL:  pageURL,
		Title:    title,
	}
	data, _ := json.Marshal(output)
	return string(data), nil
}

// buildAXTreeWithRefs builds a text accessibility tree with [ref=N] markers on
// interactive elements. Returns the tree string and the ref→entry mapping.
func buildAXTreeWithRefs(nodes []accessibilityNodeExt) (string, map[int]refEntry) {
	if len(nodes) == 0 {
		return "(empty accessibility tree)", nil
	}

	nodesByID := make(map[string]*accessibilityNodeExt, len(nodes))
	for index := range nodes {
		nodesByID[nodes[index].NodeID] = &nodes[index]
	}

	refs := make(map[int]refEntry)
	nextRef := 1

	var builder strings.Builder
	var walk func(id string, depth int)
	walk = func(id string, depth int) {
		node, ok := nodesByID[id]
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

		// Assign a ref to interactive elements that have a backendDOMNodeId.
		var refMarker string
		if isInteractiveRole(role) && node.BackendDOMNodeID != 0 {
			ref := nextRef
			nextRef++
			refs[ref] = refEntry{
				BackendDOMNodeID: node.BackendDOMNodeID,
				Role:             role,
				Name:             name,
			}
			refMarker = fmt.Sprintf("[ref=%d] ", ref)
		}

		line := indent + refMarker + role
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

// getPageMetadata retrieves the current page URL and title via CDP.
func getPageMetadata(ctx context.Context, browser browsers.Browser, sessionId string) (string, string) {
	result, err := browser.SendCDPCommand(ctx, "Runtime.evaluate", map[string]interface{}{
		"expression":    `JSON.stringify({url: location.href, title: document.title})`,
		"returnByValue": true,
	}, sessionId)
	if err != nil {
		return "", ""
	}
	var evalResponse struct {
		Result struct {
			Value json.RawMessage `json:"value"`
		} `json:"result"`
	}
	if err := json.Unmarshal(result, &evalResponse); err != nil {
		return "", ""
	}
	var metadata struct {
		URL   string `json:"url"`
		Title string `json:"title"`
	}
	// The value may be a JSON string that needs double-unmarshal.
	var raw string
	if json.Unmarshal(evalResponse.Result.Value, &raw) == nil {
		_ = json.Unmarshal([]byte(raw), &metadata)
	} else {
		_ = json.Unmarshal(evalResponse.Result.Value, &metadata)
	}
	return metadata.URL, metadata.Title
}
