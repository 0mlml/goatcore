// picker-e2e.mjs — Playwright regression test for the docs language picker.
//
// The picker is injected at runtime into mdBook's menu bar (theme/
// language-picker.js). When mdBook renamed #menu-bar -> #mdbook-menu-bar the
// picker silently stopped rendering — the kind of break a static check can't
// catch but a real browser can. This loads a built book and asserts the
// picker renders, lists languages, opens, marks the active language, and that
// switching computes the right /<lang>/ URL.
//
// Usage: node tests/picker-e2e.mjs <built-book-dir>   (default: book)
// Needs: the `playwright` package + a chromium browser installed.

import { chromium } from 'playwright';
import { createServer } from 'http';
import { readFile } from 'fs/promises';
import { extname, resolve, sep } from 'path';

const ROOT = resolve(process.argv[2] || 'book');
const PORT = 8799;
const MIME = {
  '.html': 'text/html', '.js': 'text/javascript', '.css': 'text/css',
  '.json': 'application/json', '.svg': 'image/svg+xml', '.woff2': 'font/woff2',
  '.woff': 'font/woff', '.ttf': 'font/ttf', '.png': 'image/png', '.ico': 'image/x-icon',
};

const server = createServer(async (req, res) => {
  try {
    let p = decodeURIComponent(req.url.split('?')[0]);
    if (p.endsWith('/')) p += 'index.html';
    // Serve only files under ROOT: a request for /../x must not read outside it.
    const file = resolve(ROOT, '.' + p);
    if (file !== ROOT && !file.startsWith(ROOT + sep)) throw new Error('outside root');
    const data = await readFile(file);
    res.writeHead(200, { 'Content-Type': MIME[extname(p)] || 'application/octet-stream' });
    res.end(data);
  } catch { res.writeHead(404); res.end('not found'); }
});
await new Promise((r) => server.listen(PORT, '127.0.0.1', r));

const browser = await chromium.launch();
const page = await browser.newPage();
const jsErrors = [];
page.on('pageerror', (e) => jsErrors.push(String(e)));

await page.goto(`http://127.0.0.1:${PORT}/`, { waitUntil: 'networkidle' });

const r = await page.evaluate(() => {
  const btn = document.querySelector('.goat-lang-button');
  const itemsBefore = [...document.querySelectorAll('.goat-lang-item')].map((a) => a.textContent.trim());
  if (btn) btn.click(); // open the dropdown
  const menu = document.querySelector('.goat-lang-menu');
  const fr = [...document.querySelectorAll('.goat-lang-item')].find((a) => /Français/.test(a.textContent));
  return {
    pickerPresent: !!document.querySelector('.goat-lang-picker'),
    buttonHasFlag: btn ? /\p{Regional_Indicator}/u.test(btn.textContent) : false,
    insertedInto: (document.querySelector('.goat-lang-picker')?.parentElement || {}).className || null,
    items: itemsBefore,
    menuOpens: !!(menu && menu.style.display !== 'none'),
    activeMarked: !!document.querySelector('.goat-lang-item.active'),
    frHref: fr ? fr.getAttribute('href') : null,
  };
});

await browser.close();
server.close();

const fail = [];
if (!r.pickerPresent) fail.push('picker did not render (.goat-lang-picker missing) — mdBook menu-bar selector likely changed');
if (!r.buttonHasFlag) fail.push('picker button shows no flag');
if (r.items.length < 2) fail.push(`expected >= 2 language items, got ${r.items.length}`);
if (!r.menuOpens) fail.push('dropdown did not open on click');
if (!r.activeMarked) fail.push('current language not marked active');
if (!r.frHref || !r.frHref.startsWith('/fr/')) fail.push(`Français link should go to /fr/, got: ${r.frHref}`);
if (jsErrors.length) fail.push('page JS errors: ' + jsErrors.join('; '));

if (fail.length) {
  console.error('PICKER E2E FAILED:\n - ' + fail.join('\n - '));
  console.error('debug:', JSON.stringify(r, null, 2));
  process.exit(1);
}
console.log(`PICKER E2E OK: ${r.items.length} languages, inserted into .${r.insertedInto}, active "${r.items[0]}", Français -> ${r.frHref}`);
