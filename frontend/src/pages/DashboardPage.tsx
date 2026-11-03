import { useQuery } from '@tanstack/react-query';

import { fetchDashboard } from '@/api/dashboard';
import { PortalHome } from '@/components/PortalHome';
import { useMe } from '@/hooks/useMe';
import { copyText } from '@/lib/brand';
import { isOrgAdmin } from '@/lib/roles';

export function DashboardPage() {
  const me = useMe();
  const q = useQuery({ queryKey: ['dashboard'], queryFn: fetchDashboard });
  const brand = q.data?.brand ?? {};

  return (
    <PortalHome
      convention={
        q.data?.copy?.convention_name || q.data?.organization?.display_name || 'Your organization'
      }
      welcome={q.data?.copy?.welcome || 'Welcome'}
      tagline={q.data?.copy?.tagline || 'Your home for inventory, events, and crew tools.'}
      firstName={(me.data?.display_name || 'there').split(' ')[0]}
      modules={q.data?.usable_modules ?? []}
      legalFooter={copyText(brand, 'legal_footer')}
      loading={q.isLoading}
      error={q.isError}
      admin={isOrgAdmin(me.data)}
    />
  );
}
