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
// field needed for ref-based interactions (used by the AX fallback path).
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
	Tree     string           `json:"tree"`
	RefCount int              `json:"refCount"`
	Refs     []domRefMetadata `json:"refs"`
	PageURL  string           `json:"pageUrl"`
	Title    string           `json:"title"`
}

// domRefMetadata holds the role and name for a single DOM-based ref.
type domRefMetadata struct {
	Role string `json:"role"`
	Name string `json:"name"`
}

// domSnapshotScript is the JavaScript injected into the page to build a
// DOM-based accessibility tree with [ref=N] markers on interactive elements.
// Element references are stored in window.__teanodeRefs for subsequent
// ref-based actions (click_ref, type_ref, hover_ref, select_option).
//
// This approach is used instead of Accessibility.getFullAXTree because the
// CDP accessibility domain is unreliable in headless Chrome environments,
// often returning only the RootWebArea node even with DOM.enable,
// DOM.getDocument(depth=-1), and Accessibility.enable.
const domSnapshotScript = `(function() {
  var refs = [null];
  var refMeta = [];
  var lines = [];

  function getRole(el) {
    var r = el.getAttribute && el.getAttribute('role');
    if (r) return r;
    var t = el.tagName;
    if (t === 'A') return 'link';
    if (t === 'BUTTON' || t === 'SUMMARY') return 'button';
    if (t === 'INPUT') {
      var tp = (el.type || 'text').toLowerCase();
      if (tp === 'checkbox') return 'checkbox';
      if (tp === 'radio') return 'radio';
      if (tp === 'range') return 'slider';
      if (tp === 'number') return 'spinbutton';
      if (tp === 'search') return 'searchbox';
      if (tp === 'submit' || tp === 'reset' || tp === 'button') return 'button';
      if (tp === 'hidden') return '';
      return 'textbox';
    }
    if (t === 'SELECT') return 'combobox';
    if (t === 'TEXTAREA') return 'textbox';
    if (t === 'OPTION') return 'option';
    if (/^H[1-6]$/.test(t)) return 'heading';
    if (t === 'IMG') return 'img';
    if (t === 'NAV') return 'navigation';
    if (t === 'MAIN') return 'main';
    if (t === 'FORM') return 'form';
    if (t === 'TABLE') return 'table';
    if (t === 'UL' || t === 'OL') return 'list';
    if (t === 'LI') return 'listitem';
    return '';
  }

  function getAccessibleName(el) {
    if (!el.getAttribute) return '';
    var name = el.getAttribute('aria-label') ||
               el.getAttribute('title') ||
               el.getAttribute('placeholder') ||
               el.getAttribute('alt') || '';
    if (!name && el.labels && el.labels.length > 0) {
      name = (el.labels[0].textContent || '').trim();
    }
    return name;
  }

  var INTERACTIVE_ROLES = {
    button:1, link:1, textbox:1, searchbox:1, combobox:1, listbox:1,
    option:1, checkbox:1, radio:1, slider:1, spinbutton:1, tab:1,
    menuitem:1, menuitemcheckbox:1, menuitemradio:1, treeitem:1
  };

  function isInteractive(role, el) {
    if (INTERACTIVE_ROLES[role]) return true;
    var ti = el.getAttribute && el.getAttribute('tabindex');
    if (ti !== null && ti !== '-1') return true;
    if (el.getAttribute && el.getAttribute('contenteditable') === 'true') return true;
    return false;
  }

  function esc(s) { return s.replace(/\\/g, '\\\\').replace(/"/g, '\\"'); }

  function indent(depth) {
    var s = '';
    for (var i = 0; i < depth; i++) s += '  ';
    return s;
  }

  function walk(node, depth) {
    if (node.nodeType === 3) {
      var text = node.textContent.trim();
      if (text) {
        lines.push(indent(depth) + 'StaticText "' + esc(text.substring(0, 200)) + '"');
      }
      return;
    }
    if (node.nodeType !== 1) return;
    var el = node;
    var t = el.tagName;
    if (!t || t === 'SCRIPT' || t === 'STYLE' || t === 'NOSCRIPT') return;
    if (el.getAttribute('aria-hidden') === 'true') return;
    try {
      var cs = getComputedStyle(el);
      if (cs.display === 'none' || cs.visibility === 'hidden') return;
    } catch(e) {}

    var role = getRole(el);
    var accessibleName = getAccessibleName(el);

    if (!role && !accessibleName) {
      var ch = el.childNodes;
      for (var i = 0; i < ch.length; i++) walk(ch[i], depth);
      return;
    }

    var displayName = accessibleName;
    if (!displayName && (role === 'link' || role === 'button' || role === 'option' ||
        role === 'tab' || role === 'menuitem' || role === 'listitem' || role === 'heading')) {
      displayName = (el.textContent || '').trim().substring(0, 100);
    }

    var refMarker = '';
    if (isInteractive(role, el)) {
      var ref = refs.length;
      refs.push(el);
      refMeta.push({role: role, name: displayName || ''});
      refMarker = '[ref=' + ref + '] ';
    }

    var line = indent(depth) + refMarker + role;
    if (displayName) line += ' "' + esc(displayName) + '"';

    if (role === 'heading') {
      var level = el.getAttribute('aria-level') || (t && t.charAt(1));
      if (level) line += ' (level ' + level + ')';
    }
    if (el.checked) line += ' checked=true';
    if (el.disabled) line += ' disabled';
    if (el.required) line += ' required';
    var expanded = el.getAttribute && el.getAttribute('aria-expanded');
    if (expanded !== null && expanded !== undefined) line += ' expanded=' + expanded;
    if (el.getAttribute && el.getAttribute('aria-selected') === 'true') line += ' selected';
    if (role === 'textbox' || role === 'searchbox' || role === 'spinbutton') {
      line += ' value="' + esc(el.value || '') + '"';
    }

    lines.push(line);

    if (t === 'SELECT') {
      var ci = indent(depth + 1);
      for (var j = 0; j < el.options.length; j++) {
        var opt = el.options[j];
        var oref = refs.length;
        refs.push(opt);
        refMeta.push({role: 'option', name: (opt.textContent || '').trim()});
        var oLine = ci + '[ref=' + oref + '] option "' + esc((opt.textContent || '').trim()) + '"';
        if (opt.selected) oLine += ' selected';
        lines.push(oLine);
      }
      return;
    }

    var children = el.childNodes;
    for (var k = 0; k < children.length; k++) walk(children[k], depth + 1);
  }

  var root = document.body || document.documentElement;
  if (root) {
    lines.push('RootWebArea "' + esc(document.title || '') + '"');
    var ch = root.childNodes;
    for (var i = 0; i < ch.length; i++) walk(ch[i], 1);
  }

  window.__teanodeRefs = refs;
  return {
    tree: lines.join('\n'),
    refCount: refs.length - 1,
    refs: refMeta,
    pageUrl: location.href,
    title: document.title
  };
})()`

// executeEnhancedSnapshot performs a DOM-based snapshot that assigns stable
// integer refs to interactive elements. The refs are stored both in the
// browser's window.__teanodeRefs array (for DOM resolution) and in
// globalRefStore (for metadata like role and name).
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
		return "", fmt.Errorf("DOM snapshot evaluation: %w", err)
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
		return "", fmt.Errorf("parsing DOM snapshot response: %w", err)
	}
	if evalResponse.ExceptionDetails != nil {
		return "", fmt.Errorf("DOM snapshot error: %s", evalResponse.ExceptionDetails.Text)
	}

	var snapshot domSnapshotResponse
	if err := json.Unmarshal(evalResponse.Result.Value, &snapshot); err != nil {
		return "", fmt.Errorf("parsing DOM snapshot value: %w", err)
	}

	// Populate the server-side ref store with metadata from the DOM walker.
	// The actual element references live in window.__teanodeRefs in the browser.
	refs := make(map[int]refEntry, len(snapshot.Refs))
	for index, metadata := range snapshot.Refs {
		refs[index+1] = refEntry{
			Role: metadata.Role,
			Name: metadata.Name,
		}
	}
	globalRefStore.store(sessionId, refs)

	output := snapshotResult{
		Tree:     snapshot.Tree,
		RefCount: snapshot.RefCount,
		PageURL:  snapshot.PageURL,
		Title:    snapshot.Title,
	}
	data, _ := json.Marshal(output)
	return string(data), nil
}

// buildAXTreeWithRefs builds a text accessibility tree with [ref=N] markers on
// interactive elements from CDP Accessibility.getFullAXTree nodes. This is the
// AX-based tree builder, kept as a tested utility. The primary snapshot path
// now uses the DOM-based approach (executeEnhancedSnapshot) which is more
// reliable in headless Chrome environments.
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
