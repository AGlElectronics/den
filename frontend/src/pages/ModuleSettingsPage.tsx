import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useEffect, useState, type ReactNode } from 'react';
import { AlertTriangle } from 'lucide-react';

import { ApiError } from '@/api/client';
import {
  compactSettings,
  getModuleSettings,
  putModuleSettings,
  type LostFound,
  type ModuleSettingsData,
  type SellerSettings,
  type SmtpSecurity,
  type TasksSettings,
} from '@/api/moduleSettings';

const input = 'mt-1 w-full rounded-md border border-den-border bg-den-surface px-2 py-1';
const card = 'space-y-3 rounded-lg border border-den-border bg-den-surface p-4 text-sm';

function num(v: string): number | undefined {
  if (v.trim() === '') return undefined;
  const n = Number(v);
  return Number.isFinite(n) ? n : Number.NaN;
}

function Field({ label, hint, error, children }: { label: string; hint?: string; error?: string; children: ReactNode }) {
  return (
    <label className="block">
      {label}
      {children}
      {hint && <span className="block text-xs text-den-foreground/60">{hint}</span>}
      {error && <span className="block text-xs text-den-danger">{error}</span>}
    </label>
  );
}

/**
 * Den → Module settings: values Den hands to paired modules through the
 * integration bundle. Empty fields mean "the module decides".
 */
export function ModuleSettingsPage() {
  const qc = useQueryClient();
  const settings = useQuery({ queryKey: ['module-settings'], queryFn: getModuleSettings });
  const [d, setD] = useState<ModuleSettingsData>({});
  const [password, setPassword] = useState('');
  const [clearPassword, setClearPassword] = useState(false);
  useEffect(() => {
    if (settings.data) setD(settings.data.data);
  }, [settings.data]);

  const save = useMutation({
    mutationFn: () => {
      const body = compactSettings(d);
      if (password) body.smtp = { ...(body.smtp ?? {}), password };
      else if (clearPassword) body.smtp = { ...(body.smtp ?? {}), password: null };
      return putModuleSettings(body);
    },
    onSuccess: (s) => {
      setPassword('');
      setClearPassword(false);
      qc.setQueryData(['module-settings'], s);
    },
  });
  const errors = save.error instanceof ApiError ? ((save.error.details as Record<string, string>) ?? {}) : {};

  const set = <K extends keyof ModuleSettingsData>(section: K, patch: Partial<NonNullable<ModuleSettingsData[K]>>) =>
    setD((cur) => ({ ...cur, [section]: { ...(cur[section] ?? {}), ...patch } }));
  const lf = d.stash?.lost_found;
  const setLF = (v: LostFound | undefined) => setD((cur) => ({ ...cur, stash: { ...(cur.stash ?? {}), lost_found: v } }));
  const seller: SellerSettings = d.finance?.seller ?? {};
  const setSeller = (patch: Partial<SellerSettings>) =>
    setD((cur) => ({ ...cur, finance: { ...(cur.finance ?? {}), seller: { ...(cur.finance?.seller ?? {}), ...patch } } }));
  const setTasks = <K extends keyof TasksSettings>(part: K, patch: Partial<NonNullable<TasksSettings[K]>>) =>
    setD((cur) => ({ ...cur, tasks: { ...(cur.tasks ?? {}), [part]: { ...(cur.tasks?.[part] ?? {}), ...patch } } }));

  return (
    <form
      className="max-w-4xl space-y-6"
      onSubmit={(e) => {
        e.preventDefault();
        save.mutate();
      }}
    >
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-2xl font-heading font-bold">Module settings</h1>
          <p className="mt-1 text-sm text-den-foreground/70">
            Settings Den hands to every paired module (each module only gets the parts it uses). Paired modules pick up a
            change within a minute and show these values as &quot;Managed in Den&quot;. Leave a field empty to let the
            module decide.
          </p>
        </div>
        <div className="flex items-center gap-3">
          {settings.data?.updated_at && (
            <span className="text-xs text-den-foreground/60">Saved {new Date(settings.data.updated_at).toLocaleString()}</span>
          )}
          <button
            type="submit"
            className="rounded-md bg-den-primary px-3 py-2 text-sm font-medium text-den-on-primary hover:bg-den-primary-hover disabled:opacity-50"
            disabled={save.isPending || settings.isLoading}
          >
            {save.isPending ? 'Saving…' : 'Save'}
          </button>
        </div>
      </div>
      {save.isError && Object.keys(errors).length === 0 && <p className="text-sm text-den-danger">{(save.error as Error).message}</p>}
      {Object.keys(errors).length > 0 && (
        <p className="text-sm text-den-danger">Some values are not valid; see the messages below.</p>
      )}

      <section className={card}>
        <h2 className="font-heading text-base font-bold">Retention</h2>
        <p className="flex items-start gap-2 rounded-md border border-den-warn-border bg-den-warn-bg p-2 text-xs text-den-warn">
          <AlertTriangle size={14} className="mt-0.5 shrink-0" />
          Pending legal review: these periods have not been checked against data-protection law yet. Modules show the
          same warning.
        </p>
        <div className="grid gap-3 md:grid-cols-3">
          <Field label="Offboarded volunteers (days)" hint="HR deletes their records this long after offboarding." error={errors['retention.offboarded_volunteers_days']}>
            <input className={input} inputMode="numeric" value={d.retention?.offboarded_volunteers_days ?? ''}
              onChange={(e) => set('retention', { offboarded_volunteers_days: num(e.target.value) })} />
          </Field>
          <Field label="Inactive accounts (days)" hint="Accounts unused this long are removed." error={errors['retention.inactive_users_days']}>
            <input className={input} inputMode="numeric" value={d.retention?.inactive_users_days ?? ''}
              onChange={(e) => set('retention', { inactive_users_days: num(e.target.value) })} />
          </Field>
          <Field label="Module data (days)" hint="Old records in the modules are deleted after this." error={errors['retention.module_data_days']}>
            <input className={input} inputMode="numeric" value={d.retention?.module_data_days ?? ''}
              onChange={(e) => set('retention', { module_data_days: num(e.target.value) })} />
          </Field>
        </div>
      </section>

      <section className={card}>
        <h2 className="font-heading text-base font-bold">E-mail (SMTP)</h2>
        <div className="grid gap-3 md:grid-cols-2">
          <Field label="Server" error={errors['smtp.host']}>
            <input className={input} value={d.smtp?.host ?? ''} placeholder="smtp.example.org" onChange={(e) => set('smtp', { host: e.target.value })} />
          </Field>
          <Field label="Port" error={errors['smtp.port']}>
            <input className={input} inputMode="numeric" value={d.smtp?.port ?? ''} placeholder="587" onChange={(e) => set('smtp', { port: num(e.target.value) })} />
          </Field>
          <Field label="Encryption" hint="STARTTLS on port 587 or TLS on port 465." error={errors['smtp.security']}>
            <select className={input} value={d.smtp?.security ?? ''} onChange={(e) => set('smtp', { security: (e.target.value || undefined) as SmtpSecurity | undefined })}>
              <option value="">STARTTLS (default)</option>
              <option value="starttls">STARTTLS</option>
              <option value="tls">TLS</option>
              <option value="none">None (development only)</option>
            </select>
          </Field>
          <Field label="Sender address" error={errors['smtp.from']}>
            <input className={input} value={d.smtp?.from ?? ''} placeholder="noreply@example.org" onChange={(e) => set('smtp', { from: e.target.value })} />
          </Field>
          <Field label="Sender name" error={errors['smtp.from_name']}>
            <input className={input} value={d.smtp?.from_name ?? ''} placeholder="Example Con" onChange={(e) => set('smtp', { from_name: e.target.value })} />
          </Field>
          <Field label="User name" error={errors['smtp.username']}>
            <input className={input} value={d.smtp?.username ?? ''} autoComplete="off" onChange={(e) => set('smtp', { username: e.target.value })} />
          </Field>
          <Field
            label="Password"
            hint={d.smtp?.password_set ? 'A password is saved. Type a new one to replace it.' : 'No password saved.'}
            error={errors['smtp.password']}
          >
            <input className={input} type="password" value={password} autoComplete="new-password" onChange={(e) => setPassword(e.target.value)} />
          </Field>
          {d.smtp?.password_set && (
            <label className="flex items-center gap-2 self-end pb-2">
              <input type="checkbox" checked={clearPassword} onChange={(e) => setClearPassword(e.target.checked)} /> Remove the saved password
            </label>
          )}
        </div>
      </section>

      <section className={card}>
        <h2 className="font-heading text-base font-bold">Finance: seller details</h2>
        <p className="text-xs text-den-foreground/60">Printed on invoices and receipts. Only the Finance module receives them.</p>
        <div className="grid gap-3 md:grid-cols-2">
          <Field label="Legal name" error={errors['finance.seller.legal_name']}>
            <input className={input} value={seller.legal_name ?? ''} placeholder="Example Con e.V." onChange={(e) => setSeller({ legal_name: e.target.value })} />
          </Field>
          <Field label="Country" hint="Two-letter code, for example AT." error={errors['finance.seller.country']}>
            <input className={input} value={seller.country ?? ''} maxLength={2} placeholder="AT" onChange={(e) => setSeller({ country: e.target.value.toUpperCase() })} />
          </Field>
          <Field label="VAT ID" error={errors['finance.seller.vat_id']}>
            <input className={input} value={seller.vat_id ?? ''} placeholder="ATU12345678" onChange={(e) => setSeller({ vat_id: e.target.value })} />
          </Field>
          <Field label="Tax number" error={errors['finance.seller.tax_number']}>
            <input className={input} value={seller.tax_number ?? ''} onChange={(e) => setSeller({ tax_number: e.target.value })} />
          </Field>
          <Field label="Registration" hint="Register and number, for example ZVR 123456789." error={errors['finance.seller.registration']}>
            <input className={input} value={seller.registration ?? ''} onChange={(e) => setSeller({ registration: e.target.value })} />
          </Field>
          <Field label="E-mail" error={errors['finance.seller.email']}>
            <input className={input} value={seller.email ?? ''} placeholder="finance@example.org" onChange={(e) => setSeller({ email: e.target.value })} />
          </Field>
          <Field label="Website" error={errors['finance.seller.website']}>
            <input className={input} value={seller.website ?? ''} placeholder="https://example.org" onChange={(e) => setSeller({ website: e.target.value })} />
          </Field>
        </div>
        <Field label="Address" error={errors['finance.seller.address']}>
          <textarea className={input} rows={2} value={seller.address ?? ''} onChange={(e) => setSeller({ address: e.target.value })} />
        </Field>
      </section>

      <section className={card}>
        <h2 className="font-heading text-base font-bold">Stash: Lost &amp; Found</h2>
        <div className="grid gap-3 md:grid-cols-2">
          <Field label="Found items are" error={errors['stash.lost_found.mode']}>
            <select
              className={input}
              value={lf?.mode ?? ''}
              onChange={(e) => {
                const v = e.target.value;
                setLF(v === 'kept_after_con' ? { mode: v, days: 30 } : v === 'deleted_after_closing' ? { mode: v, hours: 48 } : undefined);
              }}
            >
              <option value="">decided by Stash</option>
              <option value="kept_after_con">kept for a number of days after the con</option>
              <option value="deleted_after_closing">deleted a number of hours after closing</option>
            </select>
          </Field>
          {lf?.mode === 'kept_after_con' && (
            <Field label="Days after the con" error={errors['stash.lost_found.days']}>
              <input className={input} inputMode="numeric" value={lf.days ?? ''} onChange={(e) => setLF({ mode: 'kept_after_con', days: num(e.target.value) })} />
            </Field>
          )}
          {lf?.mode === 'deleted_after_closing' && (
            <Field label="Hours after closing" error={errors['stash.lost_found.hours']}>
              <input className={input} inputMode="numeric" value={lf.hours ?? ''} onChange={(e) => setLF({ mode: 'deleted_after_closing', hours: num(e.target.value) })} />
            </Field>
          )}
        </div>
      </section>

      <section className={card}>
        <h2 className="font-heading text-base font-bold">Wiki</h2>
        <Field label="Keep old page versions (days)" hint="Older revisions are deleted; the current version is always kept. 0 = keep all." error={errors['wiki.revision_retention_days']}>
          <input className={input} inputMode="numeric" value={d.wiki?.revision_retention_days ?? ''}
            onChange={(e) => set('wiki', { revision_retention_days: num(e.target.value) })} />
        </Field>
      </section>

      <section className={card}>
        <h2 className="font-heading text-base font-bold">Tasks</h2>
        <div className="grid gap-3 md:grid-cols-3">
          {(
            [
              ['trash_days', 'Trash (days)', 'Deleted tasks are purged after this. 0 = never.'],
              ['notification_days', 'Read notifications (days)', 'Read Inbox notifications are kept this long. 0 = forever.'],
              ['audit_days', 'Audit log (days)', 'Audit log entries are kept this long. 0 = forever.'],
              ['automation_run_days', 'Automation runs (days)', 'Automation run history is kept this long. 0 = forever.'],
            ] as const
          ).map(([key, label, hint]) => (
            <Field key={key} label={label} hint={hint} error={errors[`tasks.retention.${key}`]}>
              <input className={input} inputMode="numeric" value={d.tasks?.retention?.[key] ?? ''}
                onChange={(e) => setTasks('retention', { [key]: num(e.target.value) })} />
            </Field>
          ))}
          <Field label="Due-soon reminder (days before)" hint="0 = no reminders, at most 30." error={errors['tasks.reminders.due_soon_days']}>
            <input className={input} inputMode="numeric" value={d.tasks?.reminders?.due_soon_days ?? ''}
              onChange={(e) => setTasks('reminders', { due_soon_days: num(e.target.value) })} />
          </Field>
        </div>
      </section>

      <section className={card}>
        <h2 className="font-heading text-base font-bold">Rally: submission site</h2>
        <Field label="Host name" hint="Where attendees submit events, without https://." error={errors['rally.submission_site_host']}>
          <input className={input} value={d.rally?.submission_site_host ?? ''} placeholder="submit.example.org"
            onChange={(e) => set('rally', { submission_site_host: e.target.value })} />
        </Field>
      </section>

      <section className={card}>
        <h2 className="font-heading text-base font-bold">Identity links</h2>
        <p className="text-xs text-den-foreground/60">
          Links shown in Den and the modules. The admin console link replaces the Keycloak link Den used to guess from the
          sign-in address.
        </p>
        <div className="grid gap-3 md:grid-cols-2">
          <Field label="Name of your sign-in (SSO)" error={errors['identity.sso_name']}>
            <input className={input} value={d.identity?.sso_name ?? ''} placeholder="Example Con account" onChange={(e) => set('identity', { sso_name: e.target.value })} />
          </Field>
          <Field label="Manage my account (URL)" hint="Password and two-factor settings." error={errors['identity.account_console_url']}>
            <input className={input} value={d.identity?.account_console_url ?? ''} placeholder="https://auth.example.org/realms/conservices/account"
              onChange={(e) => set('identity', { account_console_url: e.target.value })} />
          </Field>
          <Field label="Identity provider admin console (URL)" hint="Only admins see this link." error={errors['identity.idp_admin_console_url']}>
            <input className={input} value={d.identity?.idp_admin_console_url ?? ''} placeholder="https://auth.example.org/admin/master/console/#/conservices"
              onChange={(e) => set('identity', { idp_admin_console_url: e.target.value })} />
          </Field>
        </div>
      </section>
    </form>
  );
}
