import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useEffect, useState } from 'react';
import { AlertTriangle, CheckCircle2, Plus, RotateCcw } from 'lucide-react';

import { ApiError } from '@/api/client';
import { getOrgSettings, ORG_SETTINGS_SCHEMA, putOrgSettings } from '@/api/orgSettings';
import { cn } from '@/lib/cn';

const pretty = (v: unknown) => JSON.stringify(v ?? {}, null, 2);

function parse(text: string): { value?: Record<string, unknown>; error?: string } {
  try {
    const v = JSON.parse(text || '{}');
    if (v === null || typeof v !== 'object' || Array.isArray(v)) return { error: 'Must be a JSON object { … }' };
    return { value: v as Record<string, unknown> };
  } catch (e) {
    return { error: (e as Error).message };
  }
}

export function OrgSettingsPage() {
  const qc = useQueryClient();
  const settings = useQuery({ queryKey: ['org-settings'], queryFn: getOrgSettings });
  const saved = settings.data ? pretty(settings.data.data) : null;
  const [text, setText] = useState('{}');
  useEffect(() => {
    if (saved !== null) setText(saved);
  }, [saved]);

  const parsed = parse(text);
  const dirty = saved !== null && text !== saved;
  const save = useMutation({
    mutationFn: () => putOrgSettings(parsed.value ?? {}),
    onSuccess: (s) => qc.setQueryData(['org-settings'], s),
  });
  const fieldErrors =
    save.error instanceof ApiError ? ((save.error.details as Record<string, string> | undefined) ?? {}) : {};

  const setKey = (key: string, value: string) => {
    if (!parsed.value) return;
    setText(pretty({ ...parsed.value, [key]: value }));
  };

  return (
    <div className="max-w-5xl space-y-6">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-2xl font-heading font-bold">Organisation</h1>
          <p className="mt-1 text-sm text-den-foreground/70">
            Settings every paired module uses. Modules show these as &quot;Managed in Den&quot; and pick up changes
            within a minute; standalone modules keep their own settings page.
          </p>
        </div>
        <div className="flex items-center gap-2">
          {settings.data?.updated_at && (
            <span className="text-xs text-den-foreground/60">Saved {new Date(settings.data.updated_at).toLocaleString()}</span>
          )}
          <button type="button" disabled={!dirty} onClick={() => saved !== null && setText(saved)}
            className="inline-flex items-center gap-1 rounded-md border border-den-border bg-den-surface px-3 py-2 text-sm disabled:opacity-40">
            <RotateCcw size={14} /> Reset
          </button>
          <button type="button" disabled={!dirty || !!parsed.error || save.isPending} onClick={() => save.mutate()}
            className="rounded-md bg-den-primary px-3 py-2 text-sm font-medium text-white hover:bg-den-primary-hover disabled:opacity-40">
            {save.isPending ? 'Saving…' : 'Save'}
          </button>
        </div>
      </div>
      {save.isError && Object.keys(fieldErrors).length === 0 && (
        <p className="text-sm text-red-700">{(save.error as Error).message}</p>
      )}

      <div className="grid overflow-hidden rounded-xl border border-den-border bg-den-surface lg:grid-cols-[minmax(0,1fr)_minmax(0,1.1fr)]">
        <div className="border-b border-den-border p-4 lg:border-b-0 lg:border-r">
          <textarea spellCheck={false} value={text} onChange={(e) => setText(e.target.value)}
            className={cn(
              'h-72 w-full rounded-md border bg-slate-50 px-3 py-2 font-mono text-xs leading-relaxed text-slate-900',
              parsed.error ? 'border-red-400' : 'border-den-border',
            )} />
          <div className="mt-2 text-xs">
            {parsed.error ? (
              <span className="inline-flex items-center gap-1 text-red-700"><AlertTriangle size={12} /> Invalid JSON: {parsed.error}</span>
            ) : (
              <span className="inline-flex items-center gap-1 text-emerald-700"><CheckCircle2 size={12} /> Valid JSON</span>
            )}
          </div>
          {Object.keys(fieldErrors).length > 0 && (
            <ul className="mt-2 space-y-1 text-xs text-red-700">
              {Object.entries(fieldErrors).map(([k, m]) => (
                <li key={k}><code className="font-mono">{k}</code>: {m}</li>
              ))}
            </ul>
          )}
        </div>
        <div className="p-4">
          <h3 className="text-sm font-semibold">Keys</h3>
          <table className="mt-2 w-full text-xs">
            <thead className="text-left text-den-foreground/50">
              <tr>
                <th className="py-1 pr-2 font-medium">Key</th>
                <th className="py-1 pr-2 font-medium">Affects</th>
                <th className="py-1 font-medium">Value</th>
              </tr>
            </thead>
            <tbody>
              {ORG_SETTINGS_SCHEMA.map((f) => {
                const raw = parsed.value?.[f.key];
                return (
                  <tr key={f.key} className="border-t border-den-border align-top">
                    <td className="py-2 pr-2"><code className="font-mono font-semibold">{f.key}</code></td>
                    <td className="py-2 pr-2 text-den-foreground/70">{f.affects}</td>
                    <td className="py-2">
                      {raw !== undefined ? (
                        <span className="font-mono">{String(raw) || <em className="text-den-foreground/40">empty</em>}</span>
                      ) : (
                        <button type="button" disabled={!!parsed.error} onClick={() => setKey(f.key, f.example)}
                          className="inline-flex items-center gap-0.5 text-den-primary hover:underline disabled:opacity-40">
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
            Missing keys use each module&apos;s default. Extra keys are kept and passed to paired modules.
          </p>
        </div>
      </div>
    </div>
  );
}
