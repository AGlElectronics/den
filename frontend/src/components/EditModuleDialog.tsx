import { useMutation } from '@tanstack/react-query';
import { useState } from 'react';

import { ApiError } from '@/api/client';
import { updateModule, type Module, type PortalVisibility } from '@/api/modules';

const input = 'mt-1 w-full rounded-md border border-den-border bg-den-surface px-2 py-1';

/** Edit a module's name, addresses, environment, visibility and on/off switch. */
export function EditModuleDialog({ module: m, onClose, onSaved }: { module: Module; onClose: () => void; onSaved: () => void }) {
  const [displayName, setDisplayName] = useState(m.display_name);
  const [baseURL, setBaseURL] = useState(m.base_url);
  const [internalURL, setInternalURL] = useState(m.internal_url);
  const [environment, setEnvironment] = useState(m.environment);
  const [visibility, setVisibility] = useState<PortalVisibility>(m.portal_visibility);
  const [enabled, setEnabled] = useState(m.enabled);
  const save = useMutation({
    mutationFn: () =>
      updateModule(m.id, {
        display_name: displayName.trim(),
        base_url: baseURL.trim(),
        internal_url: internalURL.trim(),
        environment: environment.trim(),
        portal_visibility: visibility,
        enabled,
      }),
    onSuccess: onSaved,
  });
  const fieldErrors = save.error instanceof ApiError ? ((save.error.details as Record<string, string>) ?? {}) : {};

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4" role="dialog" aria-modal="true">
      <form
        className="w-full max-w-lg space-y-3 rounded-lg border border-den-border bg-den-surface p-5 text-sm shadow-xl"
        onSubmit={(e) => {
          e.preventDefault();
          save.mutate();
        }}
      >
        <h2 className="font-heading text-lg font-bold">Edit {m.display_name}</h2>
        <label className="block">
          Display name
          <input className={input} value={displayName} onChange={(e) => setDisplayName(e.target.value)} />
        </label>
        <label className="block">
          Address (https)
          <input className={input} value={baseURL} placeholder="https://stash.example.org" onChange={(e) => setBaseURL(e.target.value)} />
          {fieldErrors.base_url && <span className="text-xs text-den-danger">{fieldErrors.base_url}</span>}
        </label>
        <label className="block">
          Server address (optional)
          <input
            className={input}
            value={internalURL}
            placeholder="https://stash-backend.conservices-stash.svc.cluster.local:8080"
            onChange={(e) => setInternalURL(e.target.value)}
          />
          <span className="block text-xs text-den-foreground/60">
            Where Den itself reaches the module (role catalog, to-dos). Empty: the address above.
          </span>
          {fieldErrors.internal_url && <span className="text-xs text-den-danger">{fieldErrors.internal_url}</span>}
        </label>
        <label className="block">
          Environment
          <input className={input} value={environment} onChange={(e) => setEnvironment(e.target.value)} />
        </label>
        <label className="block">
          Shown on the portal to
          <select className={input} value={visibility} onChange={(e) => setVisibility(e.target.value as PortalVisibility)}>
            <option value="role_holders">People with a role in the module</option>
            <option value="everyone">Every member</option>
          </select>
        </label>
        <label className="flex items-center gap-2">
          <input type="checkbox" checked={enabled} onChange={(e) => setEnabled(e.target.checked)} /> Enabled
        </label>
        {save.isError && Object.keys(fieldErrors).length === 0 && <p className="text-den-danger">{(save.error as Error).message}</p>}
        <div className="flex justify-end gap-2">
          <button type="button" className="rounded-md border border-den-border px-3 py-2" onClick={onClose}>
            Cancel
          </button>
          <button
            type="submit"
            className="rounded-md bg-den-primary px-3 py-2 text-den-on-primary hover:bg-den-primary-hover disabled:opacity-60"
            disabled={save.isPending || !displayName.trim()}
          >
            {save.isPending ? 'Saving…' : 'Save'}
          </button>
        </div>
      </form>
    </div>
  );
}
