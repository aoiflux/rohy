// Theme token registry — the single source of truth for every colour and elevation
// used by the UI. Components NEVER hardcode colours; they read CSS custom properties
// (see applyTheme) whose values come from these tokens. Two themes are defined:
// light (white background/surface, light-blue accent) and dark (black background,
// blue-black surface, dark-blue accent), per the design spec.

export const THEMES = Object.freeze({
  LIGHT: 'light',
  DARK: 'dark',
});

export const DEFAULT_THEME = THEMES.DARK;

// Each token maps to a CSS custom property `--<token>` applied on :root. Keys are
// shared across themes so a switch only swaps values, never the token set.
export const THEME_TOKENS = Object.freeze({
  [THEMES.LIGHT]: {
    'color-primary': '#1565c0',
    'color-on-primary': '#ffffff',
    'color-accent': '#42a5f5',
    'color-on-accent': '#08213b',
    'color-background': '#ffffff',
    'color-on-background': '#12181f',
    'color-surface': '#ffffff',
    'color-surface-variant': '#f1f4f8',
    'color-on-surface': '#1a2027',
    'color-on-surface-muted': '#5a6672',
    'color-outline': '#d4dae1',
    'color-error': '#c62828',
    'color-on-error': '#ffffff',
    'color-success': '#2e7d32',
    'color-warning': '#ef6c00',
    'color-scrim': 'rgba(18, 24, 31, 0.45)',
    'elevation-1': '0 1px 3px rgba(16, 24, 40, 0.12), 0 1px 2px rgba(16, 24, 40, 0.08)',
    'elevation-2': '0 4px 12px rgba(16, 24, 40, 0.14)',
    'elevation-3': '0 12px 28px rgba(16, 24, 40, 0.18)',
  },
  [THEMES.DARK]: {
    'color-primary': '#4f9df0',
    'color-on-primary': '#04101f',
    'color-accent': '#1e6fd0',
    'color-on-accent': '#e8f1fb',
    'color-background': '#000000',
    'color-on-background': '#e6edf3',
    'color-surface': '#0b1622',
    'color-surface-variant': '#132535',
    'color-on-surface': '#dbe6f0',
    'color-on-surface-muted': '#8b9aa9',
    'color-outline': '#22384c',
    'color-error': '#ef5350',
    'color-on-error': '#ffffff',
    'color-success': '#66bb6a',
    'color-warning': '#ffa726',
    'color-scrim': 'rgba(0, 0, 0, 0.6)',
    'elevation-1': '0 1px 3px rgba(0, 0, 0, 0.5)',
    'elevation-2': '0 6px 16px rgba(0, 0, 0, 0.55)',
    'elevation-3': '0 14px 32px rgba(0, 0, 0, 0.6)',
  },
});

// Non-colour design tokens applied once (theme-independent): radii, spacing scale,
// typography and motion. Kept here so components reference var(--radius-*) etc.
export const BASE_TOKENS = Object.freeze({
  'radius-sm': '6px',
  'radius-md': '10px',
  'radius-lg': '16px',
  'space-1': '4px',
  'space-2': '8px',
  'space-3': '12px',
  'space-4': '16px',
  'space-5': '24px',
  'space-6': '32px',
  'font-sans': "'Nunito', system-ui, -apple-system, Segoe UI, Roboto, sans-serif",
  'font-mono': "'Cascadia Code', ui-monospace, SFMono-Regular, Menlo, monospace",
  'motion-fast': '120ms',
  'motion-medium': '220ms',
  'motion-slow': '360ms',
  'motion-ease': 'cubic-bezier(0.2, 0, 0, 1)',
});

// applyTheme writes the token set for `name` onto the document root as CSS custom
// properties and stamps data-theme (used by any attribute-based styling). Called by
// the theme store whenever the theme changes.
export function applyTheme(name) {
  const tokens = THEME_TOKENS[name] || THEME_TOKENS[DEFAULT_THEME];
  const root = document.documentElement;
  for (const [k, v] of Object.entries(tokens)) {
    root.style.setProperty(`--${k}`, v);
  }
  for (const [k, v] of Object.entries(BASE_TOKENS)) {
    root.style.setProperty(`--${k}`, v);
  }
  root.setAttribute('data-theme', name);
}
