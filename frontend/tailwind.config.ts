import type { Config } from 'tailwindcss';

// Brand colours are runtime CSS variables (set from Den branding JSON).
// color-mix keeps Tailwind opacity modifiers (bg-den-surface/90) working.
const brand = (name: string, fallback: string) =>
  `color-mix(in srgb, var(--${name}, ${fallback}) calc(<alpha-value> * 100%), transparent)`;

export default {
  content: ['./public/index.html', './src/**/*.{ts,tsx}'],
  theme: {
    extend: {
      colors: {
        den: {
          primary: brand('den-primary', '#e44763'),
          'primary-hover': brand('den-primary-hover', '#c93a53'),
          accent: brand('den-accent', '#f7d1d9'),
          surface: brand('den-surface', '#ffffff'),
          foreground: brand('den-foreground', '#241e20'),
          muted: brand('den-muted', '#f7f4f5'),
          border: brand('den-border', '#e7dcdf'),
        },
      },
      fontFamily: {
        heading: ['var(--den-font-heading, "Montserrat Variable", Montserrat, system-ui, sans-serif)'],
        body: ['var(--den-font-body, "Inter Variable", Inter, system-ui, sans-serif)'],
      },
    },
  },
  plugins: [],
} satisfies Config;
