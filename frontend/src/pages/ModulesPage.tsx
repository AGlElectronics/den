import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { Copy, Plus, Trash2, Key } from 'lucide-react';

import {
  createModule,
  deleteModule,
  listModules,
  mintPairingToken,
  type Module,
  type PairingToken,
} from '@/api/modules';
import { cn } from '@/lib/cn';

export function ModulesPage() {
  const qc = useQueryClient();
  const modules = useQuery({ queryKey: ['modules'], queryFn: listModules });

  const [showNew, setShowNew] = useState(false);
  const [slug, setSlug] = useState('stash');
  const [displayName, setDisplayName] = useState('Stash');
  const [baseURL, setBaseURL] = useState('');
  const [environment, setEnvironment] = useState('production');

  const [lastToken, setLastToken] = useState<PairingToken | null>(null);

  const create = useMutation({
    mutationFn: createModule,
    onSuccess: () => {
      setShowNew(false);
      setSlug('stash');
      setDisplayName('Stash');
      setBaseURL('');
      setEnvironment('production');
      void qc.invalidateQueries({ queryKey: ['modules'] });
    },
  });

  const del = useMutation({
    mutationFn: deleteModule,
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['modules'] }),
  });

  const mint = useMutation({
    mutationFn: mintPairingToken,
    onSuccess: (res) => setLastToken(res),
  });

  return (
    <div className="space-y-6">
      <div className="flex items-start justify-between">
        <div>
          <h1 className="text-2xl font-heading font-bold">Modules</h1>
          <p className="text-sm text-den-foreground/70 mt-1">
            Register the modules (Stash, Events, HR …) that belong to this organization.
          </p>
        </div>
        <button
          type="button"
          className="inline-flex items-center gap-1 bg-den-primary text-white px-3 py-2 rounded-md hover:bg-den-primary-hover"
          onClick={() => setShowNew((v) => !v)}
        >
          <Plus size={16} /> New module
        </button>
      </div>

      {showNew && (
        <form
          className="bg-den-surface border border-den-border rounded-lg p-4 grid gap-3 md:grid-cols-4"
          onSubmit={(e) => {
            e.preventDefault();
            create.mutate({
              slug: slug.trim(),
              display_name: displayName.trim(),
              base_url: baseURL.trim(),
              environment: environment.trim() || 'production',
            });
          }}
        >
          <label className="text-sm">
            Slug
            <input
              className="mt-1 w-full rounded-md border border-den-border px-2 py-1"
              value={slug}
              onChange={(e) => setSlug(e.target.value)}
            />
          </label>
          <label className="text-sm">
            Display name
            <input
              className="mt-1 w-full rounded-md border border-den-border px-2 py-1"
              value={displayName}
              onChange={(e) => setDisplayName(e.target.value)}
            />
          </label>
          <label className="text-sm">
            Base URL
            <input
              className="mt-1 w-full rounded-md border border-den-border px-2 py-1"
              value={baseURL}
              placeholder="https://stash.example.org"
              onChange={(e) => setBaseURL(e.target.value)}
            />
          </label>
          <label className="text-sm">
            Environment
            <input
              className="mt-1 w-full rounded-md border border-den-border px-2 py-1"
              value={environment}
              onChange={(e) => setEnvironment(e.target.value)}
            />
          </label>
          <div className="md:col-span-4 flex gap-2">
            <button
              type="submit"
              className="bg-den-primary text-white px-3 py-2 rounded-md hover:bg-den-primary-hover"
              disabled={create.isPending}
            >
              {create.isPending ? 'Creating…' : 'Create module'}
            </button>
            <button
              type="button"
              className="px-3 py-2 rounded-md border border-den-border"
              onClick={() => setShowNew(false)}
            >
              Cancel
            </button>
          </div>
          {create.isError && (
            <div className="md:col-span-4 text-sm text-red-700">
              {(create.error as Error).message}
            </div>
          )}
        </form>
      )}

      {lastToken && <PairingTokenCallout token={lastToken} onDismiss={() => setLastToken(null)} />}

      <div className="bg-den-surface border border-den-border rounded-lg overflow-hidden">
        <table className="w-full text-sm">
          <thead className="bg-den-muted text-left text-xs uppercase tracking-wide text-den-foreground/60">
            <tr>
              <th className="px-4 py-2">Slug</th>
              <th className="px-4 py-2">Display name</th>
              <th className="px-4 py-2">Environment</th>
              <th className="px-4 py-2">Status</th>
              <th className="px-4 py-2 w-56">Actions</th>
            </tr>
          </thead>
          <tbody>
            {modules.data?.map((m: Module) => (
              <tr key={m.id} className="border-t border-den-border">
                <td className="px-4 py-2 font-mono">{m.slug}</td>
                <td className="px-4 py-2">{m.display_name}</td>
                <td className="px-4 py-2">{m.environment}</td>
                <td className="px-4 py-2">
                  <span
                    className={cn(
                      'inline-block rounded-full px-2 py-0.5 text-xs',
                      m.status === 'paired'
                        ? 'bg-green-100 text-green-800'
                        : m.status === 'unpaired'
                          ? 'bg-amber-100 text-amber-800'
                          : 'bg-slate-100 text-slate-700',
                    )}
                  >
                    {m.status}
                  </span>
                </td>
                <td className="px-4 py-2 space-x-2">
                  <button
                    type="button"
                    className="inline-flex items-center gap-1 text-den-primary hover:text-den-primary-hover"
                    onClick={() => mint.mutate(m.id)}
                    disabled={mint.isPending}
                  >
                    <Key size={14} /> Pair
                  </button>
                  <button
                    type="button"
                    className="inline-flex items-center gap-1 text-red-600 hover:text-red-700"
                    onClick={() => {
                      if (confirm(`Delete module "${m.display_name}"?`)) {
                        del.mutate(m.id);
                      }
                    }}
                  >
                    <Trash2 size={14} /> Delete
                  </button>
                </td>
              </tr>
            ))}
            {modules.data?.length === 0 && (
              <tr>
                <td colSpan={5} className="px-4 py-6 text-center text-den-foreground/60">
                  No modules yet. Click “New module” to register one.
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
    </div>
  );
}

function PairingTokenCallout({
  token,
  onDismiss,
}: {
  token: PairingToken;
  onDismiss: () => void;
}) {
  return (
    <div className="rounded-lg border border-amber-300 bg-amber-50 p-4">
      <div className="flex items-start justify-between gap-4">
        <div>
          <div className="font-medium text-amber-900">One-time pairing URL</div>
          <div className="text-xs text-amber-800 mt-1">
            This URL is shown exactly once. Paste it into the mated module’s
            “Pair with Den” screen within 15 minutes.
          </div>
          <code className="mt-3 block break-all rounded bg-white p-2 text-xs">
            {token.pairing_url}
          </code>
          <div className="text-xs text-amber-800 mt-1">
            Expires {new Date(token.expires_at).toLocaleString()}
          </div>
        </div>
        <div className="flex flex-col gap-2">
          <button
            type="button"
            onClick={() => {
              void navigator.clipboard.writeText(token.pairing_url);
            }}
            className="inline-flex items-center gap-1 bg-amber-200 hover:bg-amber-300 text-amber-900 px-3 py-1.5 rounded-md text-sm"
          >
            <Copy size={14} /> Copy
          </button>
          <button
            type="button"
            onClick={onDismiss}
            className="text-xs text-amber-800 hover:text-amber-900"
          >
            Dismiss
          </button>
        </div>
      </div>
    </div>
  );
}
