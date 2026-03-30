(function() {
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
})()
