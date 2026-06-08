import type { APIRoute } from 'astro';

// Build-time sitemap. Enumerates the static pages under src/pages and emits
// trailing-slash canonical URLs (matching <link rel="canonical">), with a
// build-date <lastmod> — so it never drifts from a hand-maintained file.
// Served at /sitemap.xml (referenced by robots.txt).
const SITE = 'https://tela.cagdas.io';

// Per-route crawl hints, keyed by canonical path. Unlisted pages fall back.
const HINTS: Record<string, { changefreq: string; priority: string }> = {
  '/': { changefreq: 'weekly', priority: '1.0' },
  '/mcp/': { changefreq: 'weekly', priority: '0.8' },
  '/pricing/': { changefreq: 'weekly', priority: '0.9' },
  '/privacy/': { changefreq: 'yearly', priority: '0.3' },
  '/terms/': { changefreq: 'yearly', priority: '0.3' },
};

export const GET: APIRoute = () => {
  const lastmod = new Date().toISOString().slice(0, 10);
  const paths = Object.keys(import.meta.glob('./**/*.astro'))
    .map((f) => f.replace(/^\.\//, '').replace(/\.astro$/, ''))
    .map((p) => (p === 'index' ? '/' : `/${p.replace(/\/index$/, '')}/`))
    .filter((p, i, a) => a.indexOf(p) === i)
    .sort();

  const urls = paths.map((p) => {
    const h = HINTS[p] ?? { changefreq: 'monthly', priority: '0.5' };
    return `  <url>
    <loc>${SITE}${p}</loc>
    <lastmod>${lastmod}</lastmod>
    <changefreq>${h.changefreq}</changefreq>
    <priority>${h.priority}</priority>
  </url>`;
  });

  const body = `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
${urls.join('\n')}
</urlset>
`;
  return new Response(body, { headers: { 'Content-Type': 'application/xml' } });
};
