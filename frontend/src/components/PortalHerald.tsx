import { Megaphone } from 'lucide-react';

import type { UsableModule } from '@/api/dashboard';

/** Slug Herald (staff notices, see docs/proposals/staff-communications.md) will pair under. */
export const HERALD_SLUG = 'herald';

function heraldModule(modules: UsableModule[]): UsableModule | null {
  return modules.find((m) => m.slug.toLowerCase() === HERALD_SLUG) ?? null;
}

/**
 * Reserved slot for the Herald "unread notices" strip.
 *
 * Herald does not exist yet and Den has no notices endpoint, so nothing is
 * fetched or faked here. Until a module with slug `herald` is paired and
 * usable by the person, this renders nothing. Once it is, it shows a quiet
 * slot with a link into Herald. When Herald ships, replace the body with the
 * unread count from its portal contract (the same pattern as portal to-dos).
 */
export function PortalHerald({ modules }: { modules: UsableModule[] }) {
  const herald = heraldModule(modules);
  if (!herald) return null;

  return (
    <section className="mt-8" aria-label="Notices from Herald" data-testid="portal-herald-slot">
      <div className="flex flex-wrap items-center justify-between gap-3 rounded-2xl border border-dashed border-den-border bg-den-surface px-4 py-3">
        <p className="inline-flex items-center gap-2 text-sm text-den-foreground/60">
          <Megaphone size={14} aria-hidden />
          Unread notices will appear here.
        </p>
        <a
          href={herald.base_url.replace(/\/$/, '')}
          target="_blank"
          rel="noreferrer"
          className="text-sm font-medium text-den-primary hover:underline"
        >
          Open {herald.display_name}
        </a>
      </div>
    </section>
  );
}
