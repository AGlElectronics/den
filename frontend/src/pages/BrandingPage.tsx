import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useEffect, useMemo, useState } from 'react';
import { AlertTriangle, CheckCircle2, Moon, Plus, RotateCcw, Sun, X } from 'lucide-react';

import {
  ASSET_ROLES,
  deleteBrandingAsset,
  getBrandingProfile,
  listBrandingAssets,
  LOGIN_BACKGROUND_ROLE,
  putBrandingProfile,
  uploadBrandingAsset,
  type LoginLayout,
} from '@/api/branding';
import { fetchDashboard, type UsableModule } from '@/api/dashboard';
import { KeycloakSyncCard } from '@/components/KeycloakSyncCard';
import { PortalHome } from '@/components/PortalHome';
import { useMe } from '@/hooks/useMe';
import {
  BRAND_SCHEMA,
  copyText,
  DARK_KEYS,
  darkCssVars,
  darkThemeHex,
  isColor,
  lightCssVars,
  lintBrand,
  type BrandJSON,
  type BrandSection,
} from '@/lib/brand';
import { cn } from '@/lib/cn';
import { resolveTheme, useThemePreference } from '@/lib/theme';

const DARK_AFFECTS: Record<string, string> = {
  primary: 'Buttons and links in dark mode.',
  primary_hover: 'Hover state of buttons and links.',
  accent: 'Icon tiles and active nav background.',
  background: 'Cards, top bar and panels.',
  muted: 'Chips and secondary panels in modules.',
  foreground: 'Body text and headings.',
  border: 'Card outlines and dividers.',
  page: 'Page background behind the cards.',
};

const FALLBACK_THEME = Object.fromEntries(BRAND_SCHEMA.theme.map((f) => [f.key, f.fallback]));

/** Every preview variable for one mode, so the page's own mode never leaks in. */
function previewVars(draft: BrandJSON, mode: 'light' | 'dark'): React.CSSProperties {
  const full: BrandJSON = { ...draft, theme: { ...FALLBACK_THEME, ...draft.theme } };
  const vars = mode === 'light' ? lightCssVars(full) : { ...lightCssVars(full), ...darkCssVars(full) };
  return { ...vars, colorScheme: mode } as React.CSSProperties;
}

const SECTIONS: { id: BrandSection; label: string; blurb: string }[] = [
  { id: 'theme', label: 'Theme', blurb: 'Colours. Each key becomes a CSS variable used across the portal.' },
  { id: 'typography', label: 'Typography', blurb: 'Font family names for headings and body text.' },
  { id: 'copy', label: 'Copy', blurb: 'Words on the portal home. Empty or missing keys fall back to defaults.' },
];

const SAMPLE_MODULES: UsableModule[] = [
  { id: 's1', slug: 'stash', display_name: 'Stash', base_url: '#', environment: 'production', status: 'paired', enabled: true },
  { id: 's2', slug: 'events', display_name: 'Events', base_url: '#', environment: 'production', status: 'paired', enabled: true },
  { id: 's3', slug: 'hr', display_name: 'HR', base_url: '#', environment: 'production', status: 'paired', enabled: true },
];

type Texts = Record<BrandSection, string>;

function parse(text: string): { value?: Record<string, unknown>; error?: string } {
  try {
    const v = JSON.parse(text || '{}');
    if (v === null || typeof v !== 'object' || Array.isArray(v)) return { error: 'Must be a JSON object { … }' };
    return { value: v as Record<string, unknown> };
  } catch (e) {
    return { error: (e as Error).message };
  }
}

const pretty = (v: unknown) => JSON.stringify(v ?? {}, null, 2);

export function BrandingPage() {
  const qc = useQueryClient();
  const me = useMe();
  const profile = useQuery({ queryKey: ['brand', 'profile'], queryFn: getBrandingProfile });
  const assets = useQuery({ queryKey: ['brand', 'assets'], queryFn: listBrandingAssets });
  const dash = useQuery({ queryKey: ['dashboard'], queryFn: fetchDashboard });

  const saved: Texts | null = useMemo(
    () =>
      profile.data
        ? {
            theme: pretty(profile.data.theme),
            typography: pretty(profile.data.typography),
            copy: pretty(profile.data.copy),
          }
        : null,
    [profile.data],
  );

  const [texts, setTexts] = useState<Texts>({ theme: '{}', typography: '{}', copy: '{}' });
  const [tab, setTab] = useState<BrandSection>('theme');

  useEffect(() => {
    if (saved) setTexts(saved);
  }, [saved]);

  const parsed = {
    theme: parse(texts.theme),
    typography: parse(texts.typography),
    copy: parse(texts.copy),
  };
  const parseErrors = (Object.keys(parsed) as BrandSection[]).filter((s) => parsed[s].error);
  const draft: BrandJSON = {
    theme: parsed.theme.value ?? {},
    typography: parsed.typography.value ?? {},
    copy: parsed.copy.value ?? {},
  };
  const issues = lintBrand(draft);
  const dirty = saved !== null && (Object.keys(texts) as BrandSection[]).some((s) => texts[s] !== saved[s]);

  const save = useMutation({
    mutationFn: () => putBrandingProfile(draft.theme!, draft.typography!, draft.copy!),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['brand', 'profile'] });
      void qc.invalidateQueries({ queryKey: ['dashboard'] });
    },
  });

  const upload = useMutation({
    mutationFn: ({ role, file }: { role: string; file: File }) => uploadBrandingAsset(role, file),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['brand', 'assets'] }),
  });
  const remove = useMutation({
    mutationFn: deleteBrandingAsset,
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['brand', 'assets'] }),
  });

  const setKey = (section: BrandSection, key: string, value: string) => {
    const cur = parsed[section].value;
    if (!cur) return;
    setTexts((t) => ({ ...t, [section]: pretty({ ...cur, [key]: value }) }));
  };

  /** Sets or (value undefined) clears a theme.dark override. */
  const setDarkKey = (key: string, value: string | undefined) => {
    const cur = parsed.theme.value;
    if (!cur) return;
    const dark = { ...(cur.dark && typeof cur.dark === 'object' ? (cur.dark as Record<string, unknown>) : {}) };
    if (value === undefined) delete dark[key];
    else dark[key] = value;
    const next = { ...cur };
    if (Object.keys(dark).length) next.dark = dark;
    else delete next.dark;
    setTexts((t) => ({ ...t, theme: pretty(next) }));
  };

  const { preference } = useThemePreference();
  const [previewMode, setPreviewMode] = useState<'light' | 'dark'>(() => resolveTheme(preference));
  const darkDraft = (draft.theme?.dark && typeof draft.theme.dark === 'object' ? draft.theme.dark : {}) as Record<string, unknown>;
  const derivedDark = darkThemeHex({ ...FALLBACK_THEME, ...draft.theme, dark: {} });

  const loginBackground = assets.data?.find((a) => a.role === LOGIN_BACKGROUND_ROLE);
  const loginLayout: LoginLayout = draft.copy?.login_layout === 'centered' ? 'centered' : 'panel';

  const active = SECTIONS.find((s) => s.id === tab)!;
  const sectionError = parsed[tab].error;
  const previewModules = dash.data?.usable_modules.length ? dash.data.usable_modules : SAMPLE_MODULES;

  return (
    <div className="max-w-6xl space-y-6">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-2xl font-heading font-bold">Branding</h1>
          <p className="mt-1 text-sm text-den-foreground/70">
            Edit the JSON, watch the preview below. Saving creates a new revision; the portal and paired
            modules pick it up from Den.
          </p>
        </div>
        <div className="flex items-center gap-2">
          {profile.data && (
            <span className="text-xs text-den-foreground/60">
              Revision {profile.data.revision} · {new Date(profile.data.updated_at).toLocaleString()}
            </span>
          )}
          <button
            type="button"
            disabled={!dirty}
            onClick={() => saved && setTexts(saved)}
            className="inline-flex items-center gap-1 rounded-md border border-den-border bg-den-surface px-3 py-2 text-sm disabled:opacity-40"
          >
            <RotateCcw size={14} /> Reset
          </button>
          <button
            type="button"
            disabled={!dirty || parseErrors.length > 0 || save.isPending}
            onClick={() => save.mutate()}
            className="rounded-md bg-den-primary px-3 py-2 text-sm font-medium text-den-on-primary hover:bg-den-primary-hover disabled:opacity-40"
          >
            {save.isPending ? 'Saving…' : 'Save new revision'}
          </button>
        </div>
      </div>
      {save.isError && <p className="text-sm text-den-danger">{(save.error as Error).message}</p>}

      <div className="overflow-hidden rounded-xl border border-den-border bg-den-surface">
        <div className="flex border-b border-den-border">
          {SECTIONS.map((s) => (
            <button
              key={s.id}
              type="button"
              onClick={() => setTab(s.id)}
              className={cn(
                'relative px-4 py-2.5 text-sm font-medium',
                tab === s.id ? 'text-den-primary' : 'text-den-foreground/60 hover:text-den-foreground',
              )}
            >
              {s.label}
              {parsed[s.id].error && <span className="ml-1 text-den-danger">•</span>}
              {tab === s.id && <span className="absolute inset-x-2 -bottom-px h-0.5 bg-den-primary" />}
            </button>
          ))}
        </div>

        <div className="grid gap-0 lg:grid-cols-[minmax(0,1fr)_minmax(0,1.1fr)]">
          <div className="border-b border-den-border p-4 lg:border-b-0 lg:border-r">
            <p className="text-xs text-den-foreground/60">{active.blurb}</p>
            <textarea
              spellCheck={false}
              className={cn(
                'mt-2 h-80 w-full rounded-md border bg-den-field px-3 py-2 font-mono text-xs leading-relaxed text-den-foreground',
                sectionError ? 'border-den-danger' : 'border-den-border',
              )}
              value={texts[tab]}
              onChange={(e) => setTexts((t) => ({ ...t, [tab]: e.target.value }))}
            />
            <div className="mt-2 text-xs">
              {sectionError ? (
                <span className="inline-flex items-center gap-1 text-den-danger">
                  <AlertTriangle size={12} /> Invalid JSON: {sectionError} — preview shows the last valid parts.
                </span>
              ) : (
                <span className="inline-flex items-center gap-1 text-den-success">
                  <CheckCircle2 size={12} /> Valid JSON
                </span>
              )}
            </div>
            {issues.filter((i) => i.section === tab).length > 0 && (
              <ul className="mt-2 space-y-1 text-xs text-den-warn">
                {issues
                  .filter((i) => i.section === tab)
                  .map((i) => (
                    <li key={i.key}>
                      <code className="font-mono">{i.key}</code>: {i.message}
                    </li>
                  ))}
              </ul>
            )}
          </div>

          <div className="p-4">
            <h3 className="text-sm font-semibold">How each key changes the look</h3>
            <table className="mt-2 w-full text-xs">
              <thead className="text-left text-den-foreground/50">
                <tr>
                  <th className="py-1 pr-2 font-medium">Key</th>
                  <th className="py-1 pr-2 font-medium">Affects</th>
                  <th className="py-1 font-medium">Value</th>
                </tr>
              </thead>
              <tbody>
                {BRAND_SCHEMA[tab].map((f) => {
                  const raw = draft[tab]?.[f.key];
                  const present = raw !== undefined;
                  const value = typeof raw === 'string' ? raw : '';
                  return (
                    <tr key={f.key} className="border-t border-den-border align-top">
                      <td className="py-2 pr-2">
                        <code className="font-mono font-semibold">{f.key}</code>
                        {f.cssVar && <div className="font-mono text-[10px] text-den-foreground/40">{f.cssVar}</div>}
                      </td>
                      <td className="py-2 pr-2 text-den-foreground/70">{f.affects}</td>
                      <td className="py-2">
                        {f.kind === 'color' ? (
                          <label className="inline-flex items-center gap-1.5">
                            <input
                              type="color"
                              disabled={!!sectionError}
                              value={isColor(value) && value.length === 7 ? value : f.fallback}
                              onChange={(e) => setKey(tab, f.key, e.target.value)}
                              className="h-6 w-7 cursor-pointer rounded border border-den-border bg-transparent p-0"
                              title="Pick a colour (writes into the JSON)"
                            />
                            <span className="font-mono">{present ? value : <em className="text-den-foreground/40">default</em>}</span>
                          </label>
                        ) : present ? (
                          <span className="font-mono">{value || <em className="text-den-foreground/40">empty</em>}</span>
                        ) : (
                          <button
                            type="button"
                            disabled={!!sectionError}
                            onClick={() => setKey(tab, f.key, f.fallback)}
                            className="inline-flex items-center gap-0.5 text-den-primary hover:underline disabled:opacity-40"
                          >
                            <Plus size={11} /> add
                          </button>
                        )}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
            {tab === 'theme' && (
              <>
                <h3 className="mt-5 flex items-center gap-1.5 text-sm font-semibold">
                  <Moon size={13} /> Dark mode
                </h3>
                <p className="mt-1 text-[11px] text-den-foreground/60">
                  Optional overrides under <code className="font-mono">theme.dark</code>. Empty keys are derived from the
                  colours above, so dark mode always matches your brand. Used by Den, every paired module and the sign-in
                  page.
                </p>
                <table className="mt-2 w-full text-xs">
                  <tbody>
                    {DARK_KEYS.map((key) => {
                      const raw = darkDraft[key];
                      const override = typeof raw === 'string' ? raw : undefined;
                      const derived = derivedDark?.[key as keyof typeof derivedDark] ?? '#000000';
                      const shown = override && isColor(override) && override.length === 7 ? override : derived;
                      return (
                        <tr key={key} className="border-t border-den-border align-top">
                          <td className="py-2 pr-2">
                            <code className="font-mono font-semibold">dark.{key}</code>
                          </td>
                          <td className="py-2 pr-2 text-den-foreground/70">{DARK_AFFECTS[key]}</td>
                          <td className="py-2">
                            <span className="inline-flex items-center gap-1.5">
                              <input
                                type="color"
                                disabled={!!sectionError}
                                value={shown}
                                onChange={(e) => setDarkKey(key, e.target.value)}
                                className="h-6 w-7 cursor-pointer rounded border border-den-border bg-transparent p-0"
                                title="Pick a dark-mode colour (writes into theme.dark)"
                              />
                              {override !== undefined ? (
                                <>
                                  <span className="font-mono">{override}</span>
                                  <button
                                    type="button"
                                    onClick={() => setDarkKey(key, undefined)}
                                    className="text-den-foreground/50 hover:text-den-foreground"
                                    title="Remove override (use the derived colour)"
                                  >
                                    <X size={12} />
                                  </button>
                                </>
                              ) : (
                                <span className="font-mono text-den-foreground/50">
                                  {derived} <em>derived</em>
                                </span>
                              )}
                            </span>
                          </td>
                        </tr>
                      );
                    })}
                  </tbody>
                </table>
              </>
            )}
            <p className="mt-3 text-[11px] text-den-foreground/50">
              Extra keys are kept and passed to paired modules, but the Den portal ignores them. Missing keys use the
              default shown by the colour picker.
            </p>
          </div>
        </div>
      </div>

      <section>
        <div className="flex items-end justify-between">
          <div>
            <h2 className="text-lg font-heading font-bold">Live preview</h2>
            <p className="text-sm text-den-foreground/60">
              Exactly what members see on the portal home, using your unsaved JSON.
              {!dash.data?.usable_modules.length && ' Sample services shown until modules are live.'}
            </p>
          </div>
          <div className="flex items-center gap-2">
            {dirty && <span className="rounded-full bg-den-warn-bg px-2 py-0.5 text-xs text-den-warn">Unsaved changes</span>}
            <div role="radiogroup" aria-label="Preview mode" className="flex rounded-full border border-den-border bg-den-surface p-0.5">
              {(['light', 'dark'] as const).map((m) => (
                <button
                  key={m}
                  type="button"
                  role="radio"
                  aria-checked={previewMode === m}
                  onClick={() => setPreviewMode(m)}
                  className={cn(
                    'inline-flex items-center gap-1 rounded-full px-2.5 py-1 text-xs font-medium capitalize',
                    previewMode === m ? 'bg-den-primary text-den-on-primary' : 'text-den-foreground/60 hover:text-den-foreground',
                  )}
                >
                  {m === 'light' ? <Sun size={12} /> : <Moon size={12} />} {m}
                </button>
              ))}
            </div>
          </div>
        </div>
        <div className="mt-3 overflow-hidden rounded-xl border border-den-border shadow-lg">
          <div className="flex items-center gap-1.5 border-b border-den-border bg-den-muted px-3 py-2">
            <span className="h-2.5 w-2.5 rounded-full bg-red-300" />
            <span className="h-2.5 w-2.5 rounded-full bg-amber-300" />
            <span className="h-2.5 w-2.5 rounded-full bg-emerald-300" />
            <span className="ml-3 rounded bg-den-surface px-2 py-0.5 text-[11px] text-den-foreground/60">{window.location.origin}/dashboard</span>
          </div>
          <div style={previewVars(draft, previewMode)} className="pointer-events-none select-none bg-den-muted text-den-foreground">
            <div className="border-b border-den-border bg-den-surface px-6 py-3 font-heading text-lg font-extrabold text-den-foreground">
              {copyText(draft, 'convention_name') || dash.data?.organization?.display_name || 'Den'}
            </div>
            <div className="px-6 py-8">
              <PortalHome
                preview
                convention={copyText(draft, 'convention_name') || dash.data?.organization?.display_name || 'Your organization'}
                welcome={copyText(draft, 'welcome') || 'Welcome'}
                tagline={copyText(draft, 'tagline') || 'Your home for inventory, events, and crew tools.'}
                firstName={(me.data?.display_name || 'there').split(' ')[0]}
                modules={previewModules}
                legalFooter={copyText(draft, 'legal_footer')}
              />
            </div>
          </div>
        </div>
      </section>

      <section>
        <h2 className="text-lg font-heading font-bold">Assets</h2>
        <p className="text-sm text-den-foreground/70">
          Logos and favicons. Paired modules and the sign-in page fetch them through Den — no module ever gets
          storage credentials. The newest upload per role is used.
        </p>
        <div className="mt-3 grid gap-3 sm:grid-cols-2">
          {ASSET_ROLES.map(({ role, hint }) => {
            const current = assets.data?.find((a) => a.role === role);
            return (
              <div key={role} className="flex items-center gap-3 rounded-lg border border-den-border bg-den-surface p-3">
                <div className="flex h-14 w-14 shrink-0 items-center justify-center overflow-hidden rounded-md border border-den-border bg-den-muted">
                  {current ? (
                    <img src={`/api/v1/public/brand/assets/${role}?v=${current.id}`} alt={role} className="max-h-full max-w-full object-contain" />
                  ) : (
                    <span className="text-[10px] text-den-foreground/40">none</span>
                  )}
                </div>
                <div className="min-w-0 flex-1 text-sm">
                  <p className="font-mono font-semibold">{role}</p>
                  <p className="text-xs text-den-foreground/60">{hint}</p>
                  <div className="mt-1 flex gap-3 text-xs">
                    <label className="cursor-pointer text-den-primary hover:underline">
                      {upload.isPending && upload.variables?.role === role ? 'Uploading…' : current ? 'Replace' : 'Upload'}
                      <input type="file" accept="image/*" className="hidden"
                        onChange={(e) => {
                          const file = e.target.files?.[0];
                          if (file) upload.mutate({ role, file });
                          e.target.value = '';
                        }} />
                    </label>
                    {current && (
                      <button type="button" className="text-den-danger hover:underline" onClick={() => remove.mutate(current.id)}>
                        Remove
                      </button>
                    )}
                  </div>
                </div>
              </div>
            );
          })}
        </div>
        {(upload.error || remove.error) && (
          <p className="mt-2 text-sm text-den-danger">{((upload.error ?? remove.error) as Error).message}</p>
        )}
        <div className="mt-3 overflow-hidden rounded-lg border border-den-border bg-den-surface">
          <table className="w-full text-sm">
            <thead className="bg-den-muted text-left text-xs uppercase tracking-wide text-den-foreground/60">
              <tr>
                <th className="px-4 py-2">Role</th>
                <th className="px-4 py-2">Content type</th>
                <th className="px-4 py-2">Size</th>
                <th className="px-4 py-2">Uploaded</th>
              </tr>
            </thead>
            <tbody>
              {assets.data?.map((a) => (
                <tr key={a.id} className="border-t border-den-border">
                  <td className="px-4 py-2 font-mono">{a.role}</td>
                  <td className="px-4 py-2">{a.content_type}</td>
                  <td className="px-4 py-2">{a.size_bytes} B</td>
                  <td className="px-4 py-2">{new Date(a.created_at).toLocaleString()}</td>
                </tr>
              ))}
              {assets.data?.length === 0 && (
                <tr>
                  <td colSpan={4} className="px-4 py-6 text-center text-den-foreground/60">
                    No branding assets uploaded yet.
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </section>

      <section>
        <h2 className="text-lg font-heading font-bold">Sign-in background</h2>
        <p className="text-sm text-den-foreground/70">
          A big picture behind every sign-in screen: the login page and the sign-in screens of Den and every paired
          module.
        </p>
        <div className="mt-3 grid gap-4 rounded-xl border border-den-border bg-den-surface p-4 lg:grid-cols-[minmax(0,1.2fr)_minmax(0,1fr)]">
          <SignInPreview
            image={loginBackground ? `/api/v1/public/brand/assets/${LOGIN_BACKGROUND_ROLE}?v=${loginBackground.id}` : undefined}
            layout={loginLayout}
            caption={copyText(draft, 'login_caption')}
          />
          <div className="space-y-4 text-sm">
            <div>
              <p className="font-semibold">Picture</p>
              <p className="text-xs text-den-foreground/60">
                Landscape JPG or WebP, about 1920 × 1080, ideally under 2 MB (10 MB at most). Live as soon as it uploads.
              </p>
              <div className="mt-1.5 flex gap-3 text-xs">
                <label className="cursor-pointer text-den-primary hover:underline">
                  {upload.isPending && upload.variables?.role === LOGIN_BACKGROUND_ROLE
                    ? 'Uploading…'
                    : loginBackground
                      ? 'Replace picture'
                      : 'Upload picture'}
                  <input
                    type="file"
                    accept="image/jpeg,image/png,image/webp,image/avif"
                    className="hidden"
                    onChange={(e) => {
                      const file = e.target.files?.[0];
                      if (file) upload.mutate({ role: LOGIN_BACKGROUND_ROLE, file });
                      e.target.value = '';
                    }}
                  />
                </label>
                {loginBackground && (
                  <button type="button" className="text-den-danger hover:underline" onClick={() => remove.mutate(loginBackground.id)}>
                    Remove
                  </button>
                )}
              </div>
            </div>
            <fieldset disabled={!!parsed.copy.error}>
              <legend className="font-semibold">Layout</legend>
              <div className="mt-1.5 grid gap-2 sm:grid-cols-2">
                {(
                  [
                    ['panel', 'Panel on the right', 'Picture fills the screen, the form sits in a panel docked right.'],
                    ['centered', 'Centred card', 'Picture fills the screen, the form stays a card in the middle.'],
                  ] as const
                ).map(([value, label, hint]) => (
                  <label
                    key={value}
                    className={cn(
                      'cursor-pointer rounded-md border p-2.5',
                      loginLayout === value ? 'border-den-primary bg-den-accent' : 'border-den-border',
                    )}
                  >
                    <span className="flex items-center gap-2 font-medium">
                      <input
                        type="radio"
                        name="login-layout"
                        checked={loginLayout === value}
                        onChange={() => setKey('copy', 'login_layout', value)}
                      />
                      {label}
                    </span>
                    <span className="mt-0.5 block text-xs text-den-foreground/60">{hint}</span>
                  </label>
                ))}
              </div>
              <p className="mt-1.5 text-xs text-den-foreground/60">Phones always get the centred card.</p>
            </fieldset>
            <label className="block">
              <span className="font-semibold">Caption</span>
              <input
                disabled={!!parsed.copy.error}
                value={copyText(draft, 'login_caption')}
                onChange={(e) => setKey('copy', 'login_caption', e.target.value)}
                placeholder="Optional, e.g. a slogan. Hidden when empty."
                className="mt-1.5 w-full rounded-md border border-den-border bg-den-field px-3 py-2"
              />
            </label>
            <p className="text-xs text-den-foreground/60">
              Layout and caption are part of the brand JSON (<code className="font-mono">copy.login_layout</code>,{' '}
              <code className="font-mono">copy.login_caption</code>): <strong>Save new revision</strong> to publish them.
            </p>
          </div>
        </div>
      </section>

      <KeycloakSyncCard />
    </div>
  );
}

const PREVIEW_CAPTION = 'text-center text-white [text-shadow:0_1px_8px_rgb(0_0_0/0.7)]';

/** Miniature of the sign-in screen with the current picture and the unsaved layout and caption. */
function SignInPreview({ image, layout, caption }: { image?: string; layout: LoginLayout; caption: string }) {
  const form = (
    <div className="w-full space-y-1.5">
      <div className="h-2 w-1/2 rounded bg-den-foreground/25" />
      <div className="h-3 rounded border border-den-border bg-den-field" />
      <div className="h-3 rounded border border-den-border bg-den-field" />
      <div className="h-3 rounded bg-den-primary" />
    </div>
  );
  return (
    <div
      className="pointer-events-none relative aspect-video select-none overflow-hidden rounded-lg border border-den-border bg-den-muted bg-cover bg-center"
      style={image ? { backgroundImage: `url(${JSON.stringify(image)})` } : undefined}
    >
      {!image && (
        <span className="absolute inset-x-0 top-3 text-center text-xs text-den-foreground/50">No picture yet: sign-in screens look as before.</span>
      )}
      {layout === 'panel' ? (
        <>
          {caption && (
            <p className={`absolute inset-y-0 left-0 right-[34%] flex items-center justify-center p-4 text-sm font-light ${PREVIEW_CAPTION}`}>
              {caption}
            </p>
          )}
          <div className="absolute inset-y-0 right-0 flex w-[34%] items-center bg-den-surface p-3 shadow-[-6px_0_18px_rgb(0_0_0/0.3)]">{form}</div>
        </>
      ) : (
        <div className={cn('absolute inset-0 flex flex-col items-center justify-center gap-2', image && 'bg-black/40')}>
          <div className="w-[34%] rounded-md border border-den-border bg-den-surface p-3 shadow">{form}</div>
          {caption && <p className={`max-w-[70%] text-xs ${PREVIEW_CAPTION}`}>{caption}</p>}
        </div>
      )}
    </div>
  );
}
