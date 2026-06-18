/**
 * Streaming markdown renderer for the chat UI.
 *
 * Uses **remend** to fix incomplete markdown during streaming (the core
 * insight from Vercel's StreamDown), then **markdown-it** with GFM for
 * proper rendering.
 *
 * Two modes:
 * - **streaming**: preprocesses with remend before rendering
 * - **static**: renders directly (completed messages)
 *
 * Code blocks are styled but not syntax-highlighted at this layer.
 * Shiki highlighting can be layered on top later via an async upgrade
 * pass (the highlighter is async, markdown-it is sync).
 */

import remend from "remend";
import MarkdownIt from "markdown-it";

// ── markdown-it instance ──────────────────────────────────────────────────

const md = new MarkdownIt({
  html: false,
  linkify: true,
  typographer: false,
});

md.enable("table");
md.enable("strikethrough");

// ── helpers ───────────────────────────────────────────────────────────────

function escapeHtml(s: string): string {
  return s
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;");
}

// ── code block post-processing ───────────────────────────────────────────
// markdown-it emits <pre><code class="language-foo">...</code></pre>.
// We rewrite to our styled <pre class="md-code"> wrapper.

const CODE_BLOCK_RE =
  /<pre><code class="language-(\w+)">([\s\S]*?)<\/code><\/pre>/g;

function styleCodeBlocks(html: string): string {
  return html.replace(CODE_BLOCK_RE, (_m, lang: string, code: string) => {
    const langAttr = lang ? ` data-lang="${escapeHtml(lang)}"` : "";
    return `<pre class="md-code"${langAttr}><code>${code}</code></pre>`;
  });
}

// ── remend options for streaming ──────────────────────────────────────────

const remendOpts = {
  links: true,
  images: true,
  bold: true,
  italic: true,
  boldItalic: true,
  inlineCode: true,
  strikethrough: true,
  katex: false,
  inlineKatex: false,
  comparisonOperators: true,
  htmlTags: true,
  setextHeadings: true,
  linkMode: "text-only" as const,
};

// ── public API ────────────────────────────────────────────────────────────

/**
 * Render markdown for a **completed** (non-streaming) message.
 */
export function renderMarkdown(text: string): string {
  if (!text) return "";
  return styleCodeBlocks(md.render(text));
}

/**
 * Render markdown for an **in-flight streaming** message.
 * Runs remend first to fix incomplete syntax, then renders.
 */
export function renderStreamingMarkdown(text: string): string {
  if (!text) return "";
  const fixed = remend(text, remendOpts);
  return styleCodeBlocks(md.render(fixed));
}
