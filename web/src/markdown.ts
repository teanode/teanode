import { marked } from 'marked';
import hljs from 'highlight.js';

function escapeHtml(str: string): string {
  return String(str)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;');
}

const renderer = {
  code(code: string, infostring: string | undefined): string {
    const lang = infostring || '';
    let highlighted: string;
    if (lang && hljs.getLanguage(lang)) {
      try {
        highlighted = hljs.highlight(code, { language: lang }).value;
      } catch {
        highlighted = escapeHtml(code);
      }
    } else {
      try {
        highlighted = hljs.highlightAuto(code).value;
      } catch {
        highlighted = escapeHtml(code);
      }
    }
    const langClass = lang ? ` language-${escapeHtml(lang)}` : '';
    const langLabel = lang ? escapeHtml(lang) : 'code';
    return (
      '<div class="code-block">' +
      `<div class="code-header"><span class="code-lang">${langLabel}</span>` +
      '<button class="copy-btn" type="button" title="Copy"><svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg></button></div>' +
      `<pre><code class="hljs${langClass}">${highlighted}</code></pre></div>`
    );
  },
};

marked.use({ breaks: true, gfm: true, renderer });

export function renderMarkdown(text: string): string {
  if (!text) return '';
  try {
    return marked.parse(text) as string;
  } catch {
    return escapeHtml(text);
  }
}

export function formatArgs(s: string): string {
  try {
    return JSON.stringify(JSON.parse(s), null, 2);
  } catch {
    return s;
  }
}

export function highlightJson(s: string): string {
  const formatted = formatArgs(s);
  try {
    return hljs.highlight(formatted, { language: 'json' }).value;
  } catch {
    return escapeHtml(formatted);
  }
}
