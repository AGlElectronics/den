import { useQuery } from '@tanstack/react-query';
import { fetchMe, type Me } from '@/api/me';

export function useMe() {
  return useQuery<Me>({
    queryKey: ['me'],
    queryFn: fetchMe,
    staleTime: 60_000,
  });
}
