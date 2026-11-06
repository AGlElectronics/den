import { Link } from '@tanstack/react-router';
import {
  ArrowUpRight,
  BookOpen,
  CalendarRange,
  ExternalLink,
  GraduationCap,
  Landmark,
  LifeBuoy,
  ListChecks,
  Package,
  Shield,
  UsersRound,
  Warehouse,
} from 'lucide-react';

import type { UsableModule } from '@/api/dashboard';
import { PortalTodos } from '@/components/PortalTodos';
import { cn } from '@/lib/cn';

function moduleIcon(slug: string) {
  switch (slug.toLowerCase()) {
    case 'stash':
      return Warehouse;
    case 'events':
    case 'rally':
      return CalendarRange;
    case 'hr':
      return UsersRound;
    case 'finance':
      return Landmark;
    case 'learn':
      return GraduationCap;
    case 'desk':
    case 'helpdesk':
      return LifeBuoy;
    case 'tasks':
      return ListChecks;
    case 'wiki':
      return BookOpen;
    default:
      return Package;
  }
}

function moduleBlurb(slug: string): string {
  switch (slug.toLowerCase()) {
    case 'stash':
      return 'Inventory, sign-outs, and lost & found';
    case 'events':
    case 'rally':
      return 'Call for papers, programme, and editions';
    case 'hr':
      return 'Crew onboarding and role provisioning';
    case 'finance':
      return 'Invoices, payments, and money tracking';
    case 'learn':
      return 'Courses, enrolment, and training progress';
    case 'desk':
    case 'helpdesk':
      return 'Answer attendee and crew emails as a team';
    case 'tasks':
      return 'Plan projects and track who is doing what';
    case 'wiki':
      return 'Pages, team spaces and shared knowledge';
    default:
      return 'Open this service';
  }
}

function ServiceCard({ mod, preview }: { mod: UsableModule; preview?: boolean }) {
  const Icon = moduleIcon(mod.slug);
  return (
    <a
      href={preview ? undefined : mod.base_url}
      target="_blank"
      rel="noreferrer"
      className="group flex flex-col gap-3 rounded-2xl border border-den-border bg-den-surface p-5 shadow-sm transition hover:-translate-y-0.5 hover:border-den-primary/40 hover:shadow-md"
    >
      <div className="flex items-start justify-between gap-3">
        <span className="inline-flex h-11 w-11 items-center justify-center rounded-xl bg-den-accent text-den-primary">
          <Icon size={22} />
        </span>
        <ExternalLink size={16} className="text-den-foreground/30 transition group-hover:text-den-primary" />
      </div>
      <div>
        <div className="font-heading text-lg font-bold tracking-tight text-den-foreground">
          {mod.display_name}
        </div>
        <p className="mt-1 text-sm text-den-foreground/65">{moduleBlurb(mod.slug)}</p>
      </div>
      <div className="mt-auto flex items-center gap-2 text-sm font-medium text-den-primary">
        Open service
        <ArrowUpRight size={14} className="transition group-hover:translate-x-0.5 group-hover:-translate-y-0.5" />
      </div>
    </a>
  );
}

export type PortalHomeProps = {
  convention: string;
  welcome: string;
  tagline: string;
  firstName: string;
  modules: UsableModule[];
  legalFooter?: string;
  loading?: boolean;
  error?: boolean;
  /** Shows the "Admin console" entry. Never shown in preview. */
  admin?: boolean;
  /** Renders inert (no navigation) for the branding preview. */
  preview?: boolean;
};

/** The member-facing portal home. Also rendered by the branding live preview. */
export function PortalHome({
  convention,
  welcome,
  tagline,
  firstName,
  modules,
  legalFooter,
  loading,
  error,
  admin,
  preview,
}: PortalHomeProps) {
  return (
    <div className="mx-auto max-w-5xl font-body text-den-foreground">
      <section className="relative overflow-hidden rounded-3xl border border-den-border bg-gradient-to-br from-den-accent/70 via-den-surface to-den-muted px-6 py-8 sm:px-8 sm:py-10">
        <div className="pointer-events-none absolute -right-10 -top-10 h-40 w-40 rounded-full bg-den-primary/10 blur-2xl" />
        <div className="flex flex-col gap-4 sm:flex-row sm:items-end sm:justify-between">
          <div>
            <p className="text-sm font-medium uppercase tracking-[0.14em] text-den-primary">{welcome}</p>
            <h1 className="mt-2 font-heading text-3xl font-extrabold tracking-tight sm:text-4xl">{convention}</h1>
            <p className="mt-3 max-w-2xl text-base text-den-foreground/70">
              Hi {firstName} — {tagline}
            </p>
          </div>
          {admin && !preview ? (
            <Link
              to="/admin"
              className="inline-flex items-center justify-center gap-2 rounded-full bg-den-foreground px-4 py-2.5 text-sm font-semibold text-den-surface shadow-sm hover:opacity-90"
            >
              <Shield size={16} />
              Admin console
            </Link>
          ) : null}
        </div>
      </section>

      {!preview && !loading && modules.length > 0 ? <PortalTodos modules={modules} /> : null}

      <section className="mt-8">
        <h2 className="font-heading text-xl font-bold">Your services</h2>
        <p className="mt-1 text-sm text-den-foreground/60">Everything your team uses, in one place.</p>

        {error && (
          <div className="mt-4 rounded-md border border-den-danger-border bg-den-danger-bg p-3 text-sm text-den-danger">
            Failed to load your services.
          </div>
        )}

        {loading ? (
          <div className="mt-4 grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
            {[1, 2, 3].map((i) => (
              <div key={i} className="h-40 animate-pulse rounded-2xl border border-den-border bg-den-surface" />
            ))}
          </div>
        ) : modules.length === 0 ? (
          <div className="mt-4 rounded-2xl border border-dashed border-den-border bg-den-surface p-8 text-center">
            <p className="font-medium">No services are ready yet</p>
            <p className="mt-1 text-sm text-den-foreground/60">
              Your organizers are still setting things up. Check back soon.
            </p>
          </div>
        ) : (
          <div className="mt-4 grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
            {modules.map((m) => (
              <ServiceCard key={m.id} mod={m} preview={preview} />
            ))}
          </div>
        )}
      </section>

      {modules.length > 0 && (
        <section className="mt-10">
          <h2 className="font-heading text-xl font-bold">Quick links</h2>
          <div className="mt-3 flex flex-wrap gap-2">
            {modules.map((m) => (
              <a
                key={m.id}
                href={preview ? undefined : m.base_url}
                target="_blank"
                rel="noreferrer"
                className={cn(
                  'inline-flex items-center gap-1.5 rounded-full border border-den-border bg-den-surface px-3.5 py-2 text-sm font-medium text-den-foreground',
                  'hover:border-den-primary/40 hover:text-den-primary',
                )}
              >
                {m.display_name}
                <ExternalLink size={12} />
              </a>
            ))}
          </div>
        </section>
      )}

      {legalFooter ? (
        <footer className="mt-12 border-t border-den-border pt-4 text-xs text-den-foreground/50">
          {legalFooter}
        </footer>
      ) : null}
    </div>
  );
}
