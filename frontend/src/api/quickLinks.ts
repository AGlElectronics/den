import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { apiRequest } from '@/api/client';

export type QuickLink = {
  id: string;
  label: string;
  url: string;
};

export type QuickLinksResponse = {
  links: QuickLink[];
  updated_at: string | null;
};

export const fetchQuickLinks = () => apiRequest<QuickLinksResponse>('/api/v1/me/quick-links');

export const saveQuickLinks = (links: Array<Pick<QuickLink, 'label' | 'url'> & { id?: string }>) =>
  apiRequest<QuickLinksResponse>('/api/v1/me/quick-links', {
    method: 'PUT',
    body: { links },
  });

export function useQuickLinks(enabled = true) {
  return useQuery({
    queryKey: ['quick-links'],
    queryFn: fetchQuickLinks,
    enabled,
    staleTime: 60_000,
  });
}

export function useSaveQuickLinks() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: saveQuickLinks,
    onSuccess: (data) => {
      qc.setQueryData(['quick-links'], data);
    },
  });
}
