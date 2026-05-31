/**
 * Lighthouse CI config — OPT-IN, SHOWCASE-ONLY profile.
 * Run via `npm run gate:lh:showcase`.
 *
 * For designated hero/marketing pages with an intentional jaw-drop moment
 * (3D/shader hero, rich orchestrated motion). It RELAXES THE PERFORMANCE BUDGET
 * ONLY. The default `lighthouserc.cjs` stays strict and is what every other page
 * is held to.
 *
 * NON-NEGOTIABLE here: Accessibility (>=0.95), CLS (<=0.1), and
 * non-composited-animations (=0) are NOT relaxed — a showcase hero earns a
 * bigger byte/LCP/TBT budget, it never earns the right to drop a11y, jank
 * layout, or run animations off the compositor. Reduced-motion is enforced
 * separately as a HARD gate in playwright/reduced-motion.spec.ts.
 *
 * SCOPE: point CILA_LH_URLS at the specific hero/marketing route(s) only — do
 * NOT apply this profile site-wide.
 *
 * Env-driven (Lighthouse JSON can't read env; a .cjs can). `/cila:init` sets:
 *   - Astro:  BASE_URL=http://localhost:4321  CILA_WEBSERVER_CMD="npm run preview"
 *   - Next:   BASE_URL=http://localhost:3000   CILA_WEBSERVER_CMD="npm run start"
 *     (Next serves the production build with `start`, NOT `preview`.)
 */

const BASE_URL = process.env.BASE_URL || 'http://localhost:4321';
const URLS = (process.env.CILA_LH_URLS || `${BASE_URL}/`)
  .split(',')
  .map((u) => u.trim())
  .filter(Boolean);
const WEB_SERVER_CMD = process.env.CILA_WEBSERVER_CMD || 'npm run preview';

const collect = {
  url: URLS,
  numberOfRuns: 3,
  startServerReadyPattern: '(localhost|ready|started|preview|Ready in)',
  startServerReadyTimeout: 120000,
  settings: {
    preset: 'desktop',
    onlyCategories: ['performance', 'accessibility', 'best-practices', 'seo'],
  },
};
if (!process.env.PLAYWRIGHT_NO_WEBSERVER) collect.startServerCommand = WEB_SERVER_CMD;

module.exports = {
  ci: {
    collect,
    assert: {
      assertions: {
        // ── Performance budget flexes for an intentional showcase hero. ──
        'categories:performance': ['warn', { minScore: 0.6 }],
        'largest-contentful-paint': ['error', { maxNumericValue: 4000 }],
        'total-blocking-time': ['error', { maxNumericValue: 600 }],
        'first-contentful-paint': ['warn', { maxNumericValue: 3000 }],
        'speed-index': ['warn', { maxNumericValue: 5800 }],
        'interaction-to-next-paint': ['warn', { maxNumericValue: 500 }],

        // A 3D/shader/motion hero legitimately ships more JS/bytes/textures.
        // Cap them so 'showcase' never becomes 'unbounded'.
        'resource-summary:script:size': ['error', { maxNumericValue: 2097152 }],
        'resource-summary:total:size': ['error', { maxNumericValue: 8388608 }],
        'uses-responsive-images': 'warn',
        'modern-image-formats': 'warn',
        'render-blocking-resources': 'warn',
        'uses-text-compression': 'error',

        // ── NON-NEGOTIABLE: match (or exceed) the strict default; never loosen.
        'categories:accessibility': ['error', { minScore: 0.95 }],
        'categories:best-practices': ['error', { minScore: 0.95 }],
        'categories:seo': ['warn', { minScore: 0.95 }],
        'cumulative-layout-shift': ['error', { maxNumericValue: 0.1 }],
        'non-composited-animations': ['error', { maxLength: 0 }],
        'unsized-images': ['error', { maxLength: 0 }],
        // Showcase 3D/WebGL pages emit benign console noise (shader warnings,
        // device-capability probes). Downgrade to advisory here only.
        'errors-in-console': 'warn',
      },
    },
    upload: {
      target: 'temporary-public-storage',
    },
  },
};
