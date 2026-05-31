/**
 * Lighthouse CI config for cila production gates — STRICT (default) profile.
 *
 * Env-driven so one config covers every stack (Lighthouse JSON can't read env;
 * a .cjs can). `/cila:init` sets these per stack:
 *   - Astro:  BASE_URL=http://localhost:4321  CILA_WEBSERVER_CMD="npm run preview"
 *   - Next:   BASE_URL=http://localhost:3000   CILA_WEBSERVER_CMD="npm run start"
 *     (Next serves the production build with `start`, NOT `preview`.)
 *
 * | Var                  | Default                  | Purpose                              |
 * |----------------------|--------------------------|--------------------------------------|
 * | BASE_URL             | http://localhost:4321    | Site under test (origin).            |
 * | CILA_LH_URLS         | `${BASE_URL}/`           | Comma-separated URLs to audit.       |
 * | CILA_WEBSERVER_CMD   | npm run preview          | Command lhci runs to boot the site.  |
 * | PLAYWRIGHT_NO_WEBSERVER | _unset_               | Set when CI already booted the server.|
 *
 * This is the budget every NON-showcase page is held to. Hero/marketing routes
 * with an intentional jaw-drop moment use `lighthouserc.showcase.cjs` instead
 * (perf budget relaxes; a11y / CLS / non-composited-animations do NOT).
 *
 * INP / responsiveness: real INP is a FIELD metric (needs actual interactions +
 * RUM, out of cila's build-time scope). Its lab proxy is TBT — asserted HARD
 * below (`total-blocking-time` <= 200ms). Lab `interaction-to-next-paint` is
 * asserted as WARN (lab INP is indicative, not authoritative).
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
// When CI already started the server, don't boot another one.
if (!process.env.PLAYWRIGHT_NO_WEBSERVER) collect.startServerCommand = WEB_SERVER_CMD;

module.exports = {
  ci: {
    collect,
    assert: {
      assertions: {
        'categories:performance': ['error', { minScore: 0.9 }],
        'categories:accessibility': ['error', { minScore: 1.0 }],
        'categories:best-practices': ['error', { minScore: 0.95 }],
        'categories:seo': ['warn', { minScore: 0.95 }],

        'largest-contentful-paint': ['error', { maxNumericValue: 2500 }],
        'cumulative-layout-shift': ['error', { maxNumericValue: 0.1 }],
        'total-blocking-time': ['error', { maxNumericValue: 200 }],
        'first-contentful-paint': ['warn', { maxNumericValue: 1800 }],
        'speed-index': ['warn', { maxNumericValue: 3400 }],
        'interaction-to-next-paint': ['warn', { maxNumericValue: 200 }],

        'non-composited-animations': ['error', { maxLength: 0 }],
        'unsized-images': ['error', { maxLength: 0 }],
        'uses-responsive-images': 'warn',
        'modern-image-formats': 'warn',
        'render-blocking-resources': 'warn',
        'uses-text-compression': 'error',
        'errors-in-console': 'error',
      },
    },
    upload: {
      target: 'temporary-public-storage',
    },
  },
};
