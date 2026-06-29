/// <reference types="vitest/config" />
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import tailwindcss from '@tailwindcss/vite';

// https://vite.dev/config/
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { storybookTest } from '@storybook/addon-vitest/vitest-plugin';
import { playwright } from '@vitest/browser-playwright';
const dirname = typeof __dirname !== 'undefined' ? __dirname : path.dirname(fileURLToPath(import.meta.url));

// More info at: https://storybook.js.org/docs/next/writing-tests/integrations/vitest-addon
export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    // M12.1 — alias the bare `refractor` specifier to `refractor/core` so
    // @milkdown/plugin-prism's static `import { refractor } from "refractor"`
    // resolves to the empty singleton (instead of refractor/lib/common.js,
    // which refractor marks `sideEffects: ['lib/common.js']` and pulls ~36
    // grammars). We register a curated 24 onto that same singleton via the
    // plugin's prismConfig ctx slice. Regex is anchored so subpath imports
    // (`refractor/javascript`, `refractor/core`) still resolve to their own
    // files.
    alias: [{ find: /^refractor$/, replacement: 'refractor/core' }],
  },
  // Pre-bundle the Storybook interaction-test API so the browser test runner
  // doesn't discover + optimize it mid-run (which reloads Vite and fails the
  // in-flight test with "Failed to fetch dynamically imported module").
  optimizeDeps: {
    include: ['storybook/test'],
  },
  server: {
    // When running an isolated verify backend (TELA_PROXY_TARGET set), also
    // expose the dev server on the LAN/tailnet and accept any Host header so it
    // can be reached as e.g. http://<your-host>:5199. Normal `make dev` is unchanged.
    ...(process.env.TELA_PROXY_TARGET
      ? { host: true as const, allowedHosts: true as const }
      : {}),
    proxy: {
      '/api': {
        // Override with TELA_PROXY_TARGET for an isolated dev/verify backend.
        target: process.env.TELA_PROXY_TARGET ?? 'http://localhost:8080',
        changeOrigin: false,
        // Swallow upstream socket resets so a dropped backend connection
        // doesn't crash the whole dev server (ECONNRESET unhandled-error).
        configure: (proxy) => {
          proxy.on('error', () => {})
        },
      },
      // Yjs collab websocket (tela-provider → /ws/pages/{id}). Without this the
      // dev editor runs Yjs-offline, which hides React+Yjs behaviour.
      '/ws': {
        target: process.env.TELA_PROXY_TARGET ?? 'http://localhost:8080',
        ws: true,
        changeOrigin: false,
        configure: (proxy) => {
          proxy.on('error', () => {})
        },
      },
    },
  },
  test: {
    projects: [{
      extends: true,
      plugins: [
      // The plugin will run tests for the stories defined in your Storybook config
      // See options at: https://storybook.js.org/docs/next/writing-tests/integrations/vitest-addon#storybooktest
      storybookTest({
        configDir: path.join(dirname, '.storybook')
      })],
      test: {
        name: 'storybook',
        browser: {
          enabled: true,
          headless: true,
          provider: playwright({}),
          instances: [{
            browser: 'chromium'
          }]
        }
      }
    }, {
      // Plain node-environment unit tests (pure logic — no DOM, no Storybook).
      // Run with `npm run test:unit`.
      test: {
        name: 'unit',
        environment: 'node',
        include: ['src/**/*.test.{ts,tsx}'],
      }
    }]
  }
});