import { useEffect } from 'react';

export type BrandJSON = {
  theme?: Record<string, unknown>;
  typography?: Record<string, unknown>;
  copy?: Record<string, unknown>;
};

export type BrandSection = 'theme' | 'typography' | 'copy';

export type BrandField = {
  key: string;
  kind: 'color' | 'font' | 'text';
  /** CSS custom property the value is written to (theme / typography). */
  cssVar?: string;
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
    { key: 'primary', kind: 'color', cssVar: '--den-primary', fallback: '#e44763', affects: 'Buttons, links, active nav item, icons on service cards.' },
    { key: 'primary_hover', kind: 'color', cssVar: '--den-primary-hover', fallback: '#c93a53', affects: 'Hover state of buttons and links.' },
    { key: 'accent', kind: 'color', cssVar: '--den-accent', fallback: '#f7d1d9', affects: 'Hero gradient start, icon tiles, active nav background.' },
    { key: 'background', kind: 'color', cssVar: '--den-surface', fallback: '#ffffff', affects: 'Cards, top bar and panels (the "surface").' },
    { key: 'muted', kind: 'color', cssVar: '--den-muted', fallback: '#f7f4f5', affects: 'Page background behind the cards, hero gradient end.' },
    { key: 'foreground', kind: 'color', cssVar: '--den-foreground', fallback: '#241e20', affects: 'Body text and headings.' },
    { key: 'border', kind: 'color', cssVar: '--den-border', fallback: '#e7dcdf', affects: 'Card outlines and dividers.' },
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

/** Applies the brand to the whole document (portal + admin console). */
export function useApplyBrand(brand: BrandJSON | undefined) {
  useEffect(() => {
    if (!brand) return;
    const root = document.documentElement;
    const vars = brandCssVars(brand);
    const all = [...BRAND_SCHEMA.theme, ...BRAND_SCHEMA.typography].map((f) => f.cssVar!);
    for (const name of all) {
      if (vars[name]) root.style.setProperty(name, vars[name]);
      else root.style.removeProperty(name);
    }
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
      if (!known.has(key)) {
        issues.push({ section, key, message: 'Not used by the Den portal (modules may still read it).' });
        continue;
      }
      const field = BRAND_SCHEMA[section].find((f) => f.key === key)!;
      if (field.kind === 'color' && !isColor(value)) {
        issues.push({ section, key, message: 'Expected a CSS colour like "#e44763".' });
      }
      if (field.kind !== 'color' && typeof value !== 'string') {
        issues.push({ section, key, message: 'Expected a string.' });
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
  return issues;
}
