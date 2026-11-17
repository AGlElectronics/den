import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';

import type { UsableModule } from '@/api/dashboard';
import { PortalHerald } from '@/components/PortalHerald';

const mod = (slug: string, name: string): UsableModule => ({
  id: slug,
  slug,
  display_name: name,
  base_url: `https://${slug}.example.org/`,
  environment: 'prod',
  status: 'paired',
  enabled: true,
});

describe('PortalHerald', () => {
  it('renders nothing when Herald is not paired', () => {
    const { container } = render(<PortalHerald modules={[mod('hr', 'HR'), mod('tasks', 'Tasks')]} />);
    expect(container.innerHTML).toBe('');
  });

  it('shows a quiet slot with a link when Herald is paired', () => {
    render(<PortalHerald modules={[mod('herald', 'Herald')]} />);
    expect(screen.getByTestId('portal-herald-slot')).toBeTruthy();
    expect(screen.getByRole('link', { name: 'Open Herald' }).getAttribute('href')).toBe('https://herald.example.org');
  });
});
