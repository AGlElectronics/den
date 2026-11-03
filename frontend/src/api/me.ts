import { apiRequest } from '@/api/client';

export type Membership = {
  organization_id: string;
  role: string;
};

export type Me = {
  id: string;
  display_name: string;
  email: string;
  issuer: string;
  subject: string;
  memberships: Membership[];
};

export function fetchMe(): Promise<Me> {
  return apiRequest<Me>('/api/v1/me');
}
