import { chromium } from 'playwright-core';
import { execSync } from 'node:child_process';

// resolve the chromium that `playwright install chromium` placed in the cache
let exe;
try { exe = execSync('node -e "console.log(require(\'@playwright/test\').chromium.executablePath())"', { cwd: process.cwd() }).toString().trim(); } catch {}

const BASE = 'http://localhost:4330/';
const OUT = 'tooling/shots';
const browser = await chromium.launch({ executablePath: exe || undefined });

async function shot(name, { width, height, theme, full = false, settle = 600, reduce = false }) {
  const ctx = await browser.newContext({
    viewport: { width, height },
    deviceScaleFactor: 2,
    colorScheme: theme === 'light' ? 'light' : 'dark',
    reducedMotion: reduce ? 'reduce' : 'no-preference',
  });
  const page = await ctx.newPage();
  await page.goto(BASE, { waitUntil: 'networkidle' });
  if (theme) await page.evaluate((t) => { document.documentElement.dataset.theme = t; }, theme);
  await page.evaluate(async () => { if (document.fonts?.ready) await document.fonts.ready; });
  await page.waitForTimeout(settle);
  await page.screenshot({ path: `${OUT}/${name}.png`, fullPage: full });
  await ctx.close();
  console.log('✓', name);
}

// hero (above the fold), both themes
await shot('hero-dark-1440', { width: 1440, height: 900, theme: 'dark' });
await shot('hero-light-1440', { width: 1440, height: 900, theme: 'light' });
// agent moment — scroll it into view + let it play
{
  const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 }, deviceScaleFactor: 2, colorScheme: 'dark' });
  const page = await ctx.newPage();
  await page.goto(BASE, { waitUntil: 'networkidle' });
  await page.evaluate(() => document.querySelector('#agents')?.scrollIntoView({ block: 'start' }));
  await page.waitForTimeout(4200); // let typing + weave resolve
  await page.screenshot({ path: `${OUT}/agent-dark-1440.png` });
  await ctx.close();
  console.log('✓ agent-dark-1440');
}
// full page dark + light (reduced-motion → reveals + weave resolve to rest)
await shot('full-dark-1440', { width: 1440, height: 1200, theme: 'dark', full: true, settle: 1200, reduce: true });
await shot('full-light-1440', { width: 1440, height: 1200, theme: 'light', full: true, settle: 1200, reduce: true });
// mobile
await shot('hero-dark-390', { width: 390, height: 844, theme: 'dark' });
await shot('full-dark-390', { width: 390, height: 844, theme: 'dark', full: true, settle: 1200, reduce: true });

await browser.close();
