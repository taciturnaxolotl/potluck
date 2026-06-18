/**
 * Theme switcher.
 *
 * Three modes — auto, light, dark — driven entirely by mutating the
 * `<meta name="color-scheme">` tag's `content`. The browser propagates that
 * to every `light-dark()` call in tokens.css, so no CSS changes are needed
 * to flip themes.
 *
 * Cycle order is auto → dark → light → auto. First tap commits to dark
 * (most users want this); third tap returns control to the OS. Don't change
 * this without updating AGENTS.md and design/design-system.md.
 *
 * The matching app.html boot script runs synchronously before any stylesheet
 * to avoid a flash of the wrong theme on load.
 */

export type Theme = "auto" | "light" | "dark";

const STORAGE_KEY = "potluck-theme";

function meta(): HTMLMetaElement | null {
  if (typeof document === "undefined") return null;
  return document.querySelector('meta[name="color-scheme"]');
}

export function currentTheme(): Theme {
  const m = meta();
  if (!m) return "auto";
  const c = m.content.trim();
  if (c === "light" || c === "dark") return c;
  return "auto";
}

export function setTheme(mode: Theme): void {
  const m = meta();
  if (!m) return;
  if (mode === "auto") {
    m.content = "light dark";
    try {
      localStorage.removeItem(STORAGE_KEY);
    } catch {
      /* ignore */
    }
    return;
  }
  m.content = mode;
  try {
    localStorage.setItem(STORAGE_KEY, mode);
  } catch {
    /* ignore */
  }
}

export function cycleTheme(): Theme {
  const next: Record<Theme, Theme> = {
    auto: "dark",
    dark: "light",
    light: "auto",
  };
  const t = next[currentTheme()];
  setTheme(t);
  return t;
}
