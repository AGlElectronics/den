import { fileURLToPath } from 'node:url';
import { defineConfig } from 'vitest/config';

// Unit and component tests (npm test). Rsbuild builds the app; Vitest only
// runs the tests, with the same "@" alias.
export default defineConfig({
  resolve: {
    alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) },
  },
  test: {
    environment: 'jsdom',
    include: ['src/**/*.test.{ts,tsx}'],
    restoreMocks: true,
  },
});
