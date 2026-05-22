# design system

The full token table lives in `web/src/lib/styles/tokens.css`. This doc
covers rationale.

## Palette

Six brand colors. Two cream-leaning warm tones, two pinks (one for light,
one for dark), and two near-blacks. The palette is small on purpose — every
component renders in terms of semantic tokens, never raw colors, so
adjusting the palette swings the whole app at once.

| Token | Hex | Job |
|---|---|---|
| `--soft-blush` | `#ffd9da` | warm tint, text on dark surfaces |
| `--blush-rose` | `#ea638c` | accent on dark mode |
| `--dark-raspberry` | `#89023e` | accent on light mode |
| `--jet-black` | `#30343f` | text on light, surface on dark |
| `--carbon-black` | `#1b2021` | page bg on dark |
| `--paper` | `#fffaf9` | page bg on light |

### Accent role swap

`--dark-raspberry` is dark enough to read as a link on the cream `--paper`
background. On `--carbon-black` the same color disappears at small sizes,
so the dark theme promotes `--blush-rose` to accent duty. The
`--accent` semantic token bakes this in:

```css
--accent: light-dark(var(--dark-raspberry), var(--blush-rose));
```

Components reference `var(--accent)`. Components never reference the raw
palette names.

## Theming

Driven entirely by `<meta name="color-scheme">` and the CSS `light-dark()`
function — no `data-theme`, no `[prefers-color-scheme]` media queries.
`web/src/lib/theme.ts` mutates the meta tag's `content`; the cascade does
the rest.

The boot script in `app.html` runs synchronously before any stylesheet,
restoring the persisted preference. That kills the flash of wrong theme.

Cycle order: `auto → dark → light → auto`. First tap commits to dark
(most users want this); the third tap returns control to the OS. Don't
change the order — it's load-bearing for muscle memory.

## Type

- **Fraunces Variable** — display headlines. Use
  `font-variation-settings: var(--fraunces-display)` (`opsz 144, SOFT 50`)
  at ~20px and up; `var(--fraunces-text)` (`opsz 9, SOFT 0`) below.
- **Inter Variable** — body and UI.
- **IBM Plex Mono** — code and all numeric stats. Static 400/500 only.
  The variable build renders tabular figures poorly at small UI sizes.

`tnum` is on by default at the root so numbers in tables don't wobble.
Override with `font-feature-settings: "tnum" 0` for prose where you want
proportional digits.

Critical woff2s are `<link rel="preload">`-ed from `+layout.svelte` to
avoid FOIT.

## Component patterns

- Sidebar + main grid, mirrors Hack Club Auth's docs layout.
- Cards: 1px `--border`, 12px radius, `--bg-surface`, breathing 1.25rem
  padding.
- Buttons: filled with `--accent`, text on accent uses `--text-on-accent`.
- All numeric stats use `.num` (mono, tabular).

Don't add a component library yet. The taxonomy is small enough that bare
CSS scales for now; if a third surface needs the same card pattern, then
extract.
