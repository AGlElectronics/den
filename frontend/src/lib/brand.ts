import { useEffect } from 'react';

export type BrandJSON = {
  theme?: Record<string, unknown>;
  typography?: Record<string, unknown>;
  copy?: Record<string, unknown>;
};

export type BrandSection = 'theme' | 'typography' | 'copy';

export type BrandField = {
  key: string;
  kind: 'color' | 'font' | 'text' | 'choice';
  /** CSS custom property the value is written to (theme / typography). */
  cssVar?: string;
  /** Allowed values of a 'choice' field. */
  options?: string[];
  fallback: string;
  affects: string;
};

/**
 * The single source of truth for how brand JSON maps onto the UI. The
 * branding editor renders this as its reference table, and applyBrand uses
 * it to write CSS variables, so docs and behaviour cannot drift.
 */
export const BRAND_SCHEMA: Record<BrandSection, BrandField[]> = {
  theme: [
    { key: 'primary', kind: 'color', cssVar: '--den-primary', fallback: '#5048e5', affects: 'Buttons, links, active nav item, icons on service cards.' },
    { key: 'primary_hover', kind: 'color', cssVar: '--den-primary-hover', fallback: '#4338ca', affects: 'Hover state of buttons and links.' },
    { key: 'accent', kind: 'color', cssVar: '--den-accent', fallback: '#e0e7ff', affects: 'Hero gradient start, icon tiles, active nav background.' },
    { key: 'background', kind: 'color', cssVar: '--den-surface', fallback: '#ffffff', affects: 'Cards, top bar and panels (the "surface").' },
    { key: 'muted', kind: 'color', cssVar: '--den-muted', fallback: '#f5f5f7', affects: 'Page background behind the cards, hero gradient end.' },
    { key: 'foreground', kind: 'color', cssVar: '--den-foreground', fallback: '#1c1b22', affects: 'Body text and headings.' },
    { key: 'border', kind: 'color', cssVar: '--den-border', fallback: '#e4e4e7', affects: 'Card outlines and dividers.' },
    // theme.dark: optional overrides for dark mode (same keys plus `page`);
    // see darkTheme below. The editor shows them in their own table.
  ],
  typography: [
    { key: 'heading_font', kind: 'font', cssVar: '--den-font-heading', fallback: 'Montserrat', affects: 'Convention name, section titles, card titles.' },
    { key: 'body_font', kind: 'font', cssVar: '--den-font-body', fallback: 'Inter', affects: 'All other text.' },
  ],
  copy: [
    { key: 'convention_name', kind: 'text', fallback: 'Organization name', affects: 'Big title in the hero. Falls back to the organization name.' },
    { key: 'welcome', kind: 'text', fallback: 'Welcome', affects: 'Small uppercase eyebrow above the title.' },
    { key: 'tagline', kind: 'text', fallback: 'Your home for inventory, events, and crew tools.', affects: 'Line under the title, after "Hi <name> —".' },
    { key: 'legal_footer', kind: 'text', fallback: '', affects: 'Small print at the bottom of the portal. Hidden when empty.' },
    {
      key: 'login_layout',
      kind: 'choice',
      options: ['panel', 'centered'],
      fallback: 'panel',
      affects: 'Sign-in screens with a background picture: "panel" docks the form on the right, "centered" keeps the card in the middle.',
    },
    { key: 'login_caption', kind: 'text', fallback: '', affects: 'Words shown over the sign-in background picture. Hidden when empty.' },
  ],
};

/** Fonts bundled with Den; anything else must be installed on the viewer's device. */
export const BUNDLED_FONTS = ['Inter', 'Montserrat'];

const FONT_STACK: Record<string, string> = {
  Inter: '"Inter Variable", Inter',
  Montserrat: '"Montserrat Variable", Montserrat',
};

function fontStack(name: string): string {
  const base = FONT_STACK[name] ?? `"${name.replace(/"/g, '')}"`;
  return `${base}, system-ui, sans-serif`;
}

const COLOR_RE = /^(#[0-9a-f]{3,8}|rgb|hsl|oklch|color\()/i;

export function isColor(v: unknown): v is string {
  return typeof v === 'string' && COLOR_RE.test(v.trim());
}

/** CSS variables for a brand. Invalid or missing values are omitted. */
export function brandCssVars(brand: BrandJSON): Record<string, string> {
  const vars: Record<string, string> = {};
  for (const f of BRAND_SCHEMA.theme) {
    const v = brand.theme?.[f.key];
    if (isColor(v)) vars[f.cssVar!] = v.trim();
  }
  for (const f of BRAND_SCHEMA.typography) {
    const v = brand.typography?.[f.key];
    if (typeof v === 'string' && v.trim()) vars[f.cssVar!] = fontStack(v.trim());
  }
  return vars;
}

export function copyText(brand: BrandJSON, key: string): string {
  const v = brand.copy?.[key];
  return typeof v === 'string' ? v : '';
}

// --- Dark mode ---------------------------------------------------------------
// theme.dark holds optional overrides with the same colour keys; anything not
// overridden is derived from the light palette. darkTheme is the same in the
// modules (lib/moduleBrand.ts) and the Keycloak login theme (brand.js), so
// every surface gets the same dark palette.

type HSL = [number, number, number];

function hexToHsl(v: unknown): HSL | null {
  if (typeof v !== 'string') return null;
  const rgb = hexToRgb(v);
  if (!rgb) return null;
  const [r, g, b] = rgb.map((c) => c / 255);
  const max = Math.max(r, g, b);
  const min = Math.min(r, g, b);
  const l = (max + min) / 2;
  let hue = 0;
  let s = 0;
  if (max !== min) {
    const d = max - min;
    s = l > 0.5 ? d / (2 - max - min) : d / (max + min);
    hue = max === r ? (g - b) / d + (g < b ? 6 : 0) : max === g ? (b - r) / d + 2 : (r - g) / d + 4;
    hue *= 60;
  }
  return [Math.round(hue), Math.round(s * 100), Math.round(l * 100)];
}

const clamp = (n: number, lo: number, hi: number) => Math.min(hi, Math.max(lo, n));
export const hslCss = ([h, s, l]: HSL) => `hsl(${h} ${s}% ${l}%)`;

function hslToHex([h, s, l]: HSL): string {
  const a = (s / 100) * Math.min(l / 100, 1 - l / 100);
  const f = (n: number) => {
    const k = (n + h / 30) % 12;
    const c = l / 100 - a * Math.max(-1, Math.min(k - 3, 9 - k, 1));
    return Math.round(c * 255).toString(16).padStart(2, '0');
  };
  return `#${f(0)}${f(8)}${f(4)}`;
}

/** Readable text colour on top of an HSL colour. */
function onColor(c: HSL): HSL {
  return luminance(hexToRgb(hslToHex(c))!) > 0.45 ? [240, 10, 10] : [0, 0, 100];
}

const THEME_KEYS = ['primary', 'primary_hover', 'accent', 'background', 'muted', 'foreground', 'border'] as const;
type ThemeKey = (typeof THEME_KEYS)[number];
export type DarkTheme = Record<ThemeKey | 'page', HSL>;

/**
 * Dark colours from the theme: theme.dark overrides win, everything else is
 * derived from the light palette. Surfaces are near-black tinted with the
 * brand hue; the primary is lifted so it reads on dark. `background` is the
 * card surface and `page` sits behind it. Null when the theme sets no colours.
 */
export function darkTheme(theme: Record<string, unknown>): DarkTheme | null {
  if (!THEME_KEYS.some((k) => hexToHsl(theme[k]))) return null;
  const over = (theme.dark && typeof theme.dark === 'object' ? theme.dark : {}) as Record<string, unknown>;
  const primary = hexToHsl(theme.primary) ?? [243, 75, 59];
  const bg = hexToHsl(theme.background);
  const [th, tsBase] = bg && bg[1] >= 10 ? bg : primary;
  const ts = clamp(Math.round(tsBase * 0.12), 3, 9);
  const [ph, ps, pl] = primary;
  const accent = hexToHsl(theme.accent) ?? primary;
  const d: DarkTheme = {
    primary: [ph, ps, clamp(pl, 58, 72)],
    primary_hover: [ph, ps, clamp(pl, 58, 72) + 6],
    accent: [accent[0], Math.min(accent[1], 35), 22],
    background: [th, ts, 13],
    page: [th, ts, 10],
    muted: [th, ts + 1, 18],
    foreground: [th, 10, 96],
    border: [th, ts, 22],
  };
  for (const k of THEME_KEYS) {
    const c = hexToHsl(over[k]);
    if (c) d[k] = c;
  }
  const page = hexToHsl(over.page);
  if (page) d.page = page;
  else if (hexToHsl(over.background)) d.page = [d.background[0], d.background[1], clamp(d.background[2] - 3, 0, 100)];
  if (hexToHsl(over.primary) && !hexToHsl(over.primary_hover)) {
    d.primary_hover = [d.primary[0], d.primary[1], clamp(d.primary[2] + 6, 0, 90)];
  }
  return d;
}

/** The dark palette as hex, for colour pickers and contrast checks. */
export function darkThemeHex(theme: Record<string, unknown>): Record<ThemeKey | 'page', string> | null {
  const d = darkTheme(theme);
  if (!d) return null;
  return Object.fromEntries(Object.entries(d).map(([k, v]) => [k, hslToHex(v)])) as Record<ThemeKey | 'page', string>;
}

/**
 * Den's dark-mode CSS variables. Den's light `muted` is the page behind the
 * cards, so in dark it maps to the derived `page`.
 */
export function darkCssVars(brand: BrandJSON): Record<string, string> {
  const d = darkTheme(brand.theme ?? {});
  if (!d) return {};
  return {
    '--den-primary': hslCss(d.primary),
    '--den-primary-hover': hslCss(d.primary_hover),
    '--den-on-primary': hslCss(onColor(d.primary)),
    '--den-accent': hslCss(d.accent),
    '--den-surface': hslCss(d.background),
    '--den-muted': hslCss(d.page),
    '--den-foreground': hslCss(d.foreground),
    '--den-border': hslCss(d.border),
  };
}

/** Light-mode variables, including text on primary buttons. */
export function lightCssVars(brand: BrandJSON): Record<string, string> {
  const vars = brandCssVars(brand);
  const primary = hexToHsl(brand.theme?.primary);
  if (primary) vars['--den-on-primary'] = hslCss(onColor(primary));
  return vars;
}

const block = (sel: string, vars: Record<string, string>) =>
  Object.keys(vars).length ? `${sel} {\n${Object.entries(vars).map(([k, v]) => `  ${k}: ${v};`).join('\n')}\n}\n` : '';

/** Applies the brand to the whole document (portal + admin console). */
export function useApplyBrand(brand: BrandJSON | undefined) {
  useEffect(() => {
    if (!brand) return;
    let el = document.getElementById('den-brand') as HTMLStyleElement | null;
    if (!el) {
      el = document.createElement('style');
      el.id = 'den-brand';
      document.head.appendChild(el);
    }
    // :root.dark outranks the .dark defaults in styles.css.
    el.textContent = block(':root', lightCssVars(brand)) + block(':root.dark', darkCssVars(brand));
  }, [brand]);
}

export type FieldIssue = { section: BrandSection; key: string; message: string };

function hexToRgb(hex: string): [number, number, number] | null {
  let h = hex.trim().replace('#', '');
  if (h.length === 3) h = h.split('').map((c) => c + c).join('');
  if (!/^[0-9a-f]{6}$/i.test(h)) return null;
  return [0, 2, 4].map((i) => parseInt(h.slice(i, i + 2), 16)) as [number, number, number];
}

function luminance([r, g, b]: [number, number, number]): number {
  const ch = (v: number) => {
    const s = v / 255;
    return s <= 0.03928 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4;
  };
  return 0.2126 * ch(r) + 0.7152 * ch(g) + 0.0722 * ch(b);
}

/** WCAG contrast ratio between two hex colours, or null if not hex. */
export function contrastRatio(a: string, b: string): number | null {
  const ra = hexToRgb(a);
  const rb = hexToRgb(b);
  if (!ra || !rb) return null;
  const [hi, lo] = [luminance(ra), luminance(rb)].sort((x, y) => y - x);
  return (hi + 0.05) / (lo + 0.05);
}

function themeValue(brand: BrandJSON, key: string): string {
  const v = brand.theme?.[key];
  return isColor(v) ? v : BRAND_SCHEMA.theme.find((f) => f.key === key)!.fallback;
}

/** Validation hints shown next to the JSON editor. Unknown keys are allowed. */
export function lintBrand(brand: BrandJSON): FieldIssue[] {
  const issues: FieldIssue[] = [];
  for (const section of ['theme', 'typography', 'copy'] as BrandSection[]) {
    const obj = brand[section] ?? {};
    const known = new Set(BRAND_SCHEMA[section].map((f) => f.key));
    for (const [key, value] of Object.entries(obj)) {
      if (section === 'theme' && key === 'dark') {
        issues.push(...lintDark(value));
        continue;
      }
      if (!known.has(key)) {
        issues.push({ section, key, message: 'Not used by the Den portal (modules may still read it).' });
        continue;
      }
      const field = BRAND_SCHEMA[section].find((f) => f.key === key)!;
      if (field.kind === 'color' && !isColor(value)) {
        issues.push({ section, key, message: 'Expected a CSS colour like "#5048e5".' });
      }
      if (field.kind !== 'color' && typeof value !== 'string') {
        issues.push({ section, key, message: 'Expected a string.' });
      }
      if (field.kind === 'choice' && typeof value === 'string' && !field.options?.includes(value)) {
        issues.push({ section, key, message: `Expected one of: ${field.options?.map((o) => `"${o}"`).join(', ')}.` });
      }
      if (field.kind === 'font' && typeof value === 'string' && value && !BUNDLED_FONTS.includes(value)) {
        issues.push({ section, key, message: `"${value}" is not bundled; viewers without it installed get the system font.` });
      }
    }
  }
  const fg = themeValue(brand, 'foreground');
  for (const bgKey of ['muted', 'background']) {
    const ratio = contrastRatio(fg, themeValue(brand, bgKey));
    if (ratio !== null && ratio < 4.5) {
      issues.push({
        section: 'theme',
        key: bgKey,
        message: `Text (foreground) on ${bgKey} has contrast ${ratio.toFixed(1)}:1 — below 4.5:1, hard to read.`,
      });
    }
  }
  const dark = darkThemeHex(brand.theme ?? {});
  if (dark) {
    for (const bgKey of ['page', 'background'] as const) {
      const ratio = contrastRatio(dark.foreground, dark[bgKey]);
      if (ratio !== null && ratio < 4.5) {
        issues.push({
          section: 'theme',
          key: `dark.${bgKey}`,
          message: `Dark mode: text on ${bgKey} has contrast ${ratio.toFixed(1)}:1 — below 4.5:1, hard to read.`,
        });
      }
    }
  }
  return issues;
}

/** Keys allowed under theme.dark: the light colour keys plus `page`. */
export const DARK_KEYS = [...BRAND_SCHEMA.theme.map((f) => f.key), 'page'];

function lintDark(value: unknown): FieldIssue[] {
  if (value === null || typeof value !== 'object' || Array.isArray(value)) {
    return [{ section: 'theme', key: 'dark', message: 'Expected an object of colours, e.g. { "background": "#16151a" }.' }];
  }
  const issues: FieldIssue[] = [];
  for (const [key, v] of Object.entries(value)) {
    if (!DARK_KEYS.includes(key)) {
      issues.push({ section: 'theme', key: `dark.${key}`, message: `Unknown dark key (use ${DARK_KEYS.join(', ')}).` });
    } else if (!isColor(v) || !hexToRgb(v)) {
      issues.push({ section: 'theme', key: `dark.${key}`, message: 'Expected a hex colour like "#16151a".' });
    }
  }
  return issues;
}
