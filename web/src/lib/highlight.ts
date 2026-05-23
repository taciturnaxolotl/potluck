/**
 * Thin shiki wrapper for the docs page.
 *
 * Uses shiki's CSS-variables theme so highlighted output adapts to
 * light/dark via the same `light-dark()` mechanism as the rest of the
 * design system. Token colours map to the `--code-*` design tokens
 * defined in tokens.css.
 *
 * Only loads grammars that the docs page actually uses; everything
 * else is excluded from the bundle.
 */

import { createHighlighter, createCssVariablesTheme } from 'shiki';

// Map shiki's semantic token roles to design token CSS variables.
// Each `--shiki-*` var is emitted as an inline style on token <span>s;
// the `light-dark()` wrapper makes them respect the active color scheme.
const theme = createCssVariablesTheme({
	name: 'potluck',
	variablePrefix: '--shiki-',
	variableDefaults: {},
	fontStyle: true
});

const highlighterPromise = createHighlighter({
	themes: [theme],
	langs: ['bash', 'json', 'python', 'typescript', 'http', 'powershell']
});

export type Lang = 'bash' | 'json' | 'python' | 'typescript' | 'http' | 'powershell' | 'text';

/**
 * Returns an HTML string with syntax-highlighted code, wrapped in a
 * `<code>` element. The outer `<pre>` is the caller's responsibility so
 * we can keep the existing `.codeblock` styles.
 */
export async function highlight(code: string, lang: Lang): Promise<string> {
	if (lang === 'text') {
		// Plain text — escape and return as-is; no highlight needed.
		return `<code>${escapeHtml(code)}</code>`;
	}
	const hl = await highlighterPromise;
	// codeToHtml emits a full <pre><code>…</code></pre>; we only want the
	// inner <code> so we can control the <pre> wrapper ourselves.
	const html = hl.codeToHtml(code, { lang, theme: 'potluck' });
	// Strip the outer <pre ...>…</pre>, keep the inner <code>…</code>.
	return html.replace(/^<pre[^>]*>([\s\S]*)<\/pre>$/, '$1');
}

function escapeHtml(s: string): string {
	return s
		.replace(/&/g, '&amp;')
		.replace(/</g, '&lt;')
		.replace(/>/g, '&gt;')
		.replace(/"/g, '&quot;');
}
