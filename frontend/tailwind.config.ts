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
          primary: brand('den-primary', '#5048e5'),
          'primary-hover': brand('den-primary-hover', '#4338ca'),
          accent: brand('den-accent', '#e0e7ff'),
          surface: brand('den-surface', '#ffffff'),
          foreground: brand('den-foreground', '#1c1b22'),
          muted: brand('den-muted', '#f5f5f7'),
          border: brand('den-border', '#e4e4e7'),
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
