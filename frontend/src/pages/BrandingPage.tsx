import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useEffect, useMemo, useState } from 'react';
import { AlertTriangle, CheckCircle2, Plus, RotateCcw } from 'lucide-react';

import {
  ASSET_ROLES,
  deleteBrandingAsset,
  getBrandingProfile,
  listBrandingAssets,
  putBrandingProfile,
  uploadBrandingAsset,
} from '@/api/branding';
import { fetchDashboard, type UsableModule } from '@/api/dashboard';
import { PortalHome } from '@/components/PortalHome';
import { useMe } from '@/hooks/useMe';
import {
  BRAND_SCHEMA,
  brandCssVars,
  copyText,
  isColor,
  lintBrand,
  type BrandJSON,
  type BrandSection,
} from '@/lib/brand';
import { cn } from '@/lib/cn';

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
            className="rounded-md bg-den-primary px-3 py-2 text-sm font-medium text-white hover:bg-den-primary-hover disabled:opacity-40"
          >
            {save.isPending ? 'Saving…' : 'Save new revision'}
          </button>
        </div>
      </div>
      {save.isError && <p className="text-sm text-red-700">{(save.error as Error).message}</p>}

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
              {parsed[s.id].error && <span className="ml-1 text-red-600">•</span>}
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
                'mt-2 h-80 w-full rounded-md border bg-slate-50 px-3 py-2 font-mono text-xs leading-relaxed text-slate-900',
                sectionError ? 'border-red-400' : 'border-den-border',
              )}
              value={texts[tab]}
              onChange={(e) => setTexts((t) => ({ ...t, [tab]: e.target.value }))}
            />
            <div className="mt-2 text-xs">
              {sectionError ? (
                <span className="inline-flex items-center gap-1 text-red-700">
                  <AlertTriangle size={12} /> Invalid JSON: {sectionError} — preview shows the last valid parts.
                </span>
              ) : (
                <span className="inline-flex items-center gap-1 text-emerald-700">
                  <CheckCircle2 size={12} /> Valid JSON
                </span>
              )}
            </div>
            {issues.filter((i) => i.section === tab).length > 0 && (
              <ul className="mt-2 space-y-1 text-xs text-amber-800">
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
          {dirty && <span className="rounded-full bg-amber-100 px-2 py-0.5 text-xs text-amber-900">Unsaved changes</span>}
        </div>
        <div className="mt-3 overflow-hidden rounded-xl border border-slate-300 shadow-lg">
          <div className="flex items-center gap-1.5 border-b border-slate-200 bg-slate-100 px-3 py-2">
            <span className="h-2.5 w-2.5 rounded-full bg-red-300" />
            <span className="h-2.5 w-2.5 rounded-full bg-amber-300" />
            <span className="h-2.5 w-2.5 rounded-full bg-emerald-300" />
            <span className="ml-3 rounded bg-white px-2 py-0.5 text-[11px] text-slate-500">{window.location.origin}/dashboard</span>
          </div>
          <div style={brandCssVars(draft) as React.CSSProperties} className="pointer-events-none select-none bg-den-muted">
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
                      <button type="button" className="text-red-700 hover:underline" onClick={() => remove.mutate(current.id)}>
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
          <p className="mt-2 text-sm text-red-700">{((upload.error ?? remove.error) as Error).message}</p>
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
    </div>
  );
}
