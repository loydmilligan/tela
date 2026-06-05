// Generates SEO image assets into public/: og-image.png (1200×630 social card),
// apple-touch-icon.png (180), icon-192.png, icon-512.png. Rendered with the
// Playwright chromium (brand colors inline; Geist via Google Fonts).
import { chromium } from 'playwright-core';
import { execSync } from 'node:child_process';
let exe; try { exe = execSync('node -e "console.log(require(\'@playwright/test\').chromium.executablePath())"').toString().trim(); } catch {}

// The tela folded-paper "t" — geometry shared verbatim with favicon.svg /
// frontend BrandMark / the landing header. White sheet + dark fold pocket,
// meant to sit on the #4f46e5 indigo tile.
const TMARK = `<svg viewBox="0 0 512 512" xmlns="http://www.w3.org/2000/svg">
  <path fill="#f4f3ee" d="M150 240 L196 188 Q205 178 218 178 H356 Q378 178 366 200 L332 240 Q325 250 312 250 H162 Q140 250 150 240 Z"/>
  <path fill="#f4f3ee" d="M238 250 H296 Q300 250 300 268 V396 Q300 414 281 410 L245 402 Q226 398 226 380 V272 Q226 252 238 250 Z"/>
  <path fill="#1e1b4b" fill-opacity="0.4" d="M250 250 H312 Q325 250 332 240 L304 270 Q297 278 297 290 V324 Z"/>
</svg>`;

const OG_HTML = `<!doctype html><html><head><meta charset="utf-8">
<link rel="preconnect" href="https://fonts.googleapis.com">
<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
<link href="https://fonts.googleapis.com/css2?family=Geist:wght@400;500;600;700&family=Geist+Mono:wght@400;500&display=swap" rel="stylesheet">
<style>
  *{margin:0;padding:0;box-sizing:border-box}
  html,body{width:1200px;height:630px}
  .card{width:1200px;height:630px;position:relative;overflow:hidden;
    background:#12121d;color:#f5f5fb;font-family:'Geist',sans-serif;padding:84px 80px;
    display:flex;flex-direction:column;justify-content:space-between}
  .weave{position:absolute;inset:0;
    background-image:
      repeating-linear-gradient(to right,#23233a 0,#23233a 1px,transparent 1px,transparent 30px),
      repeating-linear-gradient(to bottom,#23233a 0,#23233a 1px,transparent 1px,transparent 30px);
    -webkit-mask-image:radial-gradient(120% 95% at 75% 0%,#000 0%,transparent 70%);opacity:.55}
  .glow{position:absolute;inset:0;background:radial-gradient(46% 40% at 78% 8%,rgba(108,92,255,.28),transparent 70%)}
  .row{position:relative;display:flex;align-items:center;gap:18px}
  .mark{width:60px;height:60px;border-radius:14px;background:#4f46e5;overflow:hidden}
  .mark svg{width:100%;height:100%;display:block}
  .wordmark{font-size:34px;font-weight:600;letter-spacing:-.02em}
  .headline{position:relative;font-size:82px;font-weight:700;line-height:1.02;letter-spacing:-.03em;max-width:18ch}
  .ink{color:#8b7bff}
  .foot{position:relative;display:flex;align-items:center;justify-content:space-between}
  .pills{display:flex;gap:12px}
  .pill{font-size:21px;color:#b9b9cb;border:1px solid #34344a;border-radius:999px;padding:8px 18px}
  .url{font-family:'Geist Mono',monospace;font-size:23px;color:#8b7bff}
</style></head>
<body><div class="card">
  <div class="weave"></div><div class="glow"></div>
  <div class="row">
    <span class="mark">${TMARK}</span>
    <span class="wordmark">tela</span>
  </div>
  <h1 class="headline">The wiki your agents can <span class="ink">write to.</span></h1>
  <div class="foot">
    <div class="pills">
      <span class="pill">Markdown-native</span>
      <span class="pill">Self-hosted</span>
      <span class="pill">MCP built in</span>
    </div>
    <span class="url">tela.cagdas.io</span>
  </div>
</div></body></html>`;

const icon = (s) => `<!doctype html><html><head><meta charset="utf-8"><style>
  *{margin:0;padding:0}html,body{width:${s}px;height:${s}px}
  .i{width:${s}px;height:${s}px;background:#4f46e5;overflow:hidden}
  .i svg{width:100%;height:100%;display:block}
</style></head><body><div class="i">${TMARK}</div></body></html>`;

const browser = await chromium.launch({ executablePath: exe || undefined });

let ctx = await browser.newContext({ viewport: { width: 1200, height: 630 }, deviceScaleFactor: 1 });
let p = await ctx.newPage();
await p.setContent(OG_HTML, { waitUntil: 'networkidle' });
await p.evaluate(async () => { if (document.fonts?.ready) await document.fonts.ready; });
await p.waitForTimeout(400);
await p.screenshot({ path: 'public/og-image.png', clip: { x: 0, y: 0, width: 1200, height: 630 } });
await ctx.close();
console.log('✓ og-image.png');

for (const [s, name] of [[180, 'apple-touch-icon.png'], [192, 'icon-192.png'], [512, 'icon-512.png']]) {
  ctx = await browser.newContext({ viewport: { width: s, height: s }, deviceScaleFactor: 1 });
  p = await ctx.newPage();
  await p.setContent(icon(s), { waitUntil: 'load' });
  await p.screenshot({ path: `public/${name}`, clip: { x: 0, y: 0, width: s, height: s } });
  await ctx.close();
  console.log('✓', name);
}
await browser.close();
