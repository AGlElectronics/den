import { Link } from '@tanstack/react-router';
import { Boxes, Palette, Settings } from 'lucide-react';
import { useQuery } from '@tanstack/react-query';

import { fetchDashboard } from '@/api/dashboard';

const TOOLS = [
  {
    to: '/admin/modules',
    title: 'Modules',
    description: 'Register Stash, Events, HR and friends; mint pairing links.',
    icon: Boxes,
  },
  {
    to: '/admin/branding',
    title: 'Branding',
    description: 'Colours, fonts and portal copy, with a live preview.',
    icon: Palette,
  },
  {
    to: '/admin/settings',
    title: 'Settings',
    description: 'Auth mode and operator defaults for this Den.',
    icon: Settings,
  },
] as const;

function Stat({ label, value }: { label: string; value: number | string | undefined }) {
  return (
    <div className="rounded-xl border border-slate-200 bg-white p-4">
      <div className="text-xs font-medium uppercase tracking-wide text-slate-500">{label}</div>
      <div className="mt-1 font-heading text-2xl font-bold text-slate-900">{value ?? '…'}</div>
    </div>
  );
}

export function OrganizePage() {
  const dash = useQuery({ queryKey: ['dashboard'], queryFn: fetchDashboard });
  const d = dash.data;

  return (
    <div className="max-w-5xl space-y-8">
      <div>
        <h1 className="font-heading text-2xl font-bold text-slate-900">Overview</h1>
        <p className="mt-1 text-sm text-slate-600">
          Set up modules and the look of the portal. Members only ever see the portal home.
        </p>
      </div>

      <div className="grid gap-3 sm:grid-cols-4">
        <Stat label="Paired modules" value={d?.paired_modules} />
        <Stat label="Waiting to pair" value={d?.unpaired_modules} />
        <Stat label="Live on portal" value={d?.usable_modules.length} />
        <Stat label="People in Den" value={d?.users} />
      </div>

      {(d?.unpaired_modules ?? 0) > 0 && (
        <div className="rounded-xl border border-amber-200 bg-amber-50 p-4 text-sm text-amber-900">
          {d?.unpaired_modules} module{d?.unpaired_modules === 1 ? '' : 's'} still unpaired. Members
          won&apos;t see {d?.unpaired_modules === 1 ? 'it' : 'them'} until pairing finishes and a public URL is
          set.{' '}
          <Link to="/admin/modules" className="font-semibold underline">
            Finish pairing
          </Link>
        </div>
      )}

      <div className="grid gap-4 sm:grid-cols-3">
        {TOOLS.map(({ to, title, description, icon: Icon }) => (
          <Link
            key={to}
            to={to}
            className="group rounded-xl border border-slate-200 bg-white p-5 shadow-sm transition hover:-translate-y-0.5 hover:border-slate-400 hover:shadow-md"
          >
            <span className="inline-flex h-10 w-10 items-center justify-center rounded-lg bg-slate-100 text-slate-700">
              <Icon size={18} />
            </span>
            <div className="mt-3 font-heading text-lg font-bold text-slate-900">{title}</div>
            <p className="mt-1 text-sm text-slate-600">{description}</p>
          </Link>
        ))}
      </div>
    </div>
  );
}
