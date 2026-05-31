// @ts-check
import { defineConfig, fontProviders } from 'astro/config';
import react from '@astrojs/react';
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
  site: 'https://tela.cagdas.io',

  integrations: [react()],

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
    // View dev/preview over the LAN by hostname (fleet boxes).
    server: { allowedHosts: ['marko', 'bender', 'archer'] },
    preview: { allowedHosts: ['marko', 'bender', 'archer'] },
  },
});
