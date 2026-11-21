import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { readFile, readdir, stat } from 'node:fs/promises';
import { createServer } from 'node:http';
import { extname, join, relative, sep } from 'node:path';
import { fileURLToPath } from 'node:url';
import { chromium } from 'playwright';
import { build } from 'vite';

const workspace = fileURLToPath(new URL('../../web/explorer/', import.meta.url));
const root = join(workspace, 'fixture');
const output = join(workspace, 'dist', 'fixture');
const browserPath = process.env.MCE4D_CHROME_PATH;
const expectedSHA = process.env.MCE4D_CHROME_SHA256;
if (!browserPath || !/^[a-f\d]{64}$/.test(expectedSHA ?? '')) {
  throw Error('The browser executable and its verified SHA-256 are required');
}
if (createHash('sha256').update(await readFile(browserPath)).digest('hex') !== expectedSHA) {
  throw Error('Browser executable differs from the pinned fixture material');
}
const types = new Map([['.html', 'text/html'], ['.js', 'text/javascript'], ['.css', 'text/css']]);
await build({
  configFile: false, root, esbuild: { jsx: 'automatic' },
  build: { target: 'es2024', outDir: output, emptyOutDir: true },
});

const files = new Map();
for (const item of await readdir(output, { recursive: true, withFileTypes: true })) {
  if (!item.isFile()) continue;
  const path = join(item.parentPath, item.name);
  const key = '/' + relative(output, path).split(sep).join('/');
  if ((await stat(path)).size > 4 * 1024 * 1024 || files.size >= 16 || !types.has(extname(path))) {
    throw new Error('Unexpected fixture artifact');
  }
  files.set(key, await readFile(path));
}
const id = '01234567-89ab-cdef-0123-456789abcdef';
const message = {
  protocol_version: 1, session_id: id, document_epoch: 1, hmr_epoch: 0,
  message_id: id, reply_to: id, selection_id: null, seq: 2, type: 'PONG',
  payload: { probe_id: id },
};
const expected = createHash('sha256').update('MCE4D\0C14N1\0s10:fixture-v1o1:s3:seqi2;').digest('hex');

function listener(handler) {
  const server = createServer(handler);
  return new Promise((resolve, reject) => {
    server.once('error', reject);
    server.listen(0, '127.0.0.1', () => resolve({
      origin: 'http://127.0.0.1:' + server.address().port,
      close: () => new Promise((done, fail) => server.close(error => error ? fail(error) : done())),
    }));
  });
}

function reply(res, contentType, content, csp) {
  res.writeHead(200, {
    'Content-Type': contentType,
    'Content-Security-Policy': csp,
    'X-Content-Type-Options': 'nosniff',
    'Cache-Control': 'no-store',
  });
  res.end(content);
}

const csp = "default-src 'none'; script-src 'self'; style-src 'self' 'unsafe-inline';";
let parent;
let child;
let browser;
try {
  child = await listener((req, res) => {
    if (req.url === '/') return reply(res, 'text/html', '<script src="/peer.js"></script>',
      csp + ' frame-ancestors ' + parent.origin);
    if (req.url === '/peer.js') return reply(res, 'text/javascript',
      'parent.postMessage(' + JSON.stringify(JSON.stringify(message)) + ', ' + JSON.stringify(parent.origin) + ')', csp);
    res.writeHead(404).end();
  });
  parent = await listener((req, res) => {
    if (req.url === '/') {
      const html = files.get('/index.html').toString('utf8')
        .replace('<html lang="en">', '<html lang="en" data-frame-origin="' + child.origin + '">');
      return reply(res, 'text/html', html, csp + ' frame-src ' + child.origin);
    }
    const body = files.get(req.url);
    if (body) return reply(res, types.get(extname(req.url)), body, csp);
    res.writeHead(404).end();
  });
  browser = await chromium.launch({ executablePath: browserPath, headless: true, chromiumSandbox: true, timeout: 15000 });
  const context = await browser.newContext({
    viewport: { width: 800, height: 600 }, serviceWorkers: 'block', acceptDownloads: false,
  });
  const refused = [];
  const origins = new Set([parent.origin, child.origin]);
  await context.route('**/*', route => {
    if (origins.has(new URL(route.request().url()).origin)) return route.continue();
    refused.push(new URL(route.request().url()).origin);
    return route.abort();
  });
  const page = await context.newPage();
  page.setDefaultTimeout(10000);
  const errors = [];
  page.on('pageerror', error => errors.push(String(error)));
  await page.goto(parent.origin, { timeout: 15000 });
  await page.waitForFunction(expectedDigest => document.getElementById('wire')?.textContent === expectedDigest, expected);
  await page.locator('.react-flow__node').nth(1).waitFor();
  await page.locator('.react-flow__edge').nth(1).waitFor({ state: 'attached' });
  assert.equal(await page.locator('.react-flow__node').count(), 2);
  assert.equal(await page.locator('.react-flow__edge').count(), 2);
  assert.deepEqual(errors, []);
  assert.deepEqual(refused, []);
  assert.equal(await page.locator('iframe').getAttribute('src'), child.origin + '/');
  console.log(JSON.stringify({
    browser: browser.version(), browser_sha256: expectedSHA, fixture_bytes: [...files.values()].reduce((n, data) => n + data.length, 0),
    protocol: 'cross-origin loopback JSON PONG, CSP without unsafe-eval', reactFlowNodes: 2, directedCycleEdges: 2,
    digest: expected,
  }));
} finally {
  await browser?.close();
  await Promise.all([parent?.close(), child?.close()]);
}
