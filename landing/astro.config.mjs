// @ts-check
import { defineConfig, fontProviders } from 'astro/config';
import tailwindcss from '@tailwindcss/vite';

// tela landing — standalone static marketing site. Built separately from the
// app (backend/ + frontend/), deployed as static files served at the apex.
//
// Tailwind v4 is wired through @tailwindcss/vite; all design tokens live in
// src/styles/tokens.css via the v4 @theme block (single source of truth).
//
// Fonts: Geist (display+body) + Geist Mono, self-hosted at build time via the
// Astro Fonts API with metric-matched fallbacks (size-adjust) — kills FOUT and
// keeps CLS ~0. cssVariables feed tokens.css (@theme --font-* → var(--af-*)).
export default defineConfig({
  output: 'static',
  site: 'https://telawiki.com',

  // Inline all CSS into the HTML so first paint has no render-blocking
  // stylesheet round-trip (Lighthouse "render-blocking resources"). Styles are
  // unchanged — only where they load — so the design/token/a11y gates still
  // hold. Right tradeoff for a marketing site optimizing first-visit LCP/FCP.
  build: { inlineStylesheets: 'always' },

  // Vanity redirect → the tela Blog (a public tela space, served by the app).
  redirects: {
    '/blog': '/public/spaces/59',
  },

  fonts: [
    {
      provider: fontProviders.google(),
      name: 'Geist',
      cssVariable: '--af-display',
      weights: [400, 500, 600, 700],
      subsets: ['latin'],
      fallbacks: ['ui-sans-serif', 'system-ui', 'sans-serif'],
    },
    {
      provider: fontProviders.google(),
      name: 'Geist',
      cssVariable: '--af-body',
      weights: [400, 500, 600],
      subsets: ['latin'],
      fallbacks: ['ui-sans-serif', 'system-ui', 'sans-serif'],
    },
    {
      provider: fontProviders.google(),
      name: 'Geist Mono',
      cssVariable: '--af-mono',
      weights: [400, 500],
      subsets: ['latin'],
      fallbacks: ['ui-monospace', 'SFMono-Regular', 'monospace'],
    },
  ],

  vite: {
    plugins: [tailwindcss()],
    // View dev/preview over the LAN by hostname: set ASTRO_ALLOWED_HOSTS to a
    // comma-separated host list (empty default → localhost only).
    server: { allowedHosts: (process.env.ASTRO_ALLOWED_HOSTS ?? '').split(',').map((s) => s.trim()).filter(Boolean) },
    preview: { allowedHosts: (process.env.ASTRO_ALLOWED_HOSTS ?? '').split(',').map((s) => s.trim()).filter(Boolean) },
  },
});
