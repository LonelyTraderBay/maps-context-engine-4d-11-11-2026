import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { once } from 'node:events';
import { readFile } from 'node:fs/promises';
import { createServer } from 'node:http';
import { fileURLToPath } from 'node:url';
import { chromium } from 'playwright';
import { build } from 'vite';
import { decodeBridgeMessage } from '../../web/explorer/src/bridge.ts';

const browserPath = process.env.MCE4D_T086_CHROMIUM;
const browserDigest = '24eb0a14de64ab0c9c2e283a5512ca817db64af8fd853cdb4d818f19a8e23b9b';
assert.equal(createHash('sha256').update(await readFile(browserPath)).digest('hex'), browserDigest);
const entry = fileURLToPath(new URL('./preview_sdk_fixture.mjs', import.meta.url));
const compiled = await build({ configFile: false, logLevel: 'silent',
  define: { 'process.env.NODE_ENV': JSON.stringify('production') },
  build: { write: false, minify: 'esbuild', lib: { entry, formats: ['es'], fileName: 'preview-sdk-fixture' } } });
const bundle = (Array.isArray(compiled) ? compiled[0] : compiled).output;
assert.equal(bundle.length, 1);
assert.ok(bundle[0].code.length < 1 << 20);
const html = `<!doctype html><meta charset="utf-8"><title>Preview SDK synthetic fixture</title>
<style>
body{margin:8px}
#scroll-area {height:230px;width:480px;overflow:auto}
#buttons {display:grid;grid-template-columns:repeat(10, 32px);gap:4px;zoom:1.1}
#buttons button {margin:0;height:22px;width:32px}
#outside {display:flex;align-items:flex-start;gap:150px;margin-top:12px}
#react-root {display:flex;gap:70px}
.group {width:110px;height:60px;background:#ddd}
#known-host {width:40px;height:32px;background:#777}
#shadow-host {display:block;width:180px;height:30px}
</style>
<div id="scroll-area"><form id="buttons"></form></div>
<div id="outside"><div id="react-root"></div></div>
<div id="portal"></div><test-shadow id="shadow-host"></test-shadow>
<script type="module" src="/fixture.js"></script>`;
const server = createServer((req, res) => {
  res.setHeader('Content-Security-Policy', "default-src 'none'; script-src 'self'; style-src 'unsafe-inline'");
  res.setHeader('Cache-Control', 'no-store');
  if (req.url === '/') {
    res.writeHead(200, { 'Content-Type': 'text/html' }).end(html);
  } else if (req.url === '/fixture.js') {
    res.writeHead(200, { 'Content-Type': 'text/javascript' }).end(bundle[0].code);
  } else {
    res.writeHead(404).end();
  }
});
server.listen(0, '127.0.0.1');
let browser;
try {
  await once(server, 'listening');
  const origin = 'http://127.0.0.1:' + server.address().port;
  // The owned runner supplies a private process and network namespace.
  browser = await chromium.launch({ executablePath: browserPath, headless: true, chromiumSandbox: false, timeout: 15000,
    env: { ...process.env, LD_LIBRARY_PATH: process.env.MCE4D_T086_LIBS } });
  const context = await browser.newContext({ viewport: { width: 850, height: 600 },
    deviceScaleFactor: 2, serviceWorkers: 'block', acceptDownloads: false });
  const refused = [];
  await context.route('**/*', route => {
    const incoming = new URL(route.request().url()).origin;
    if (incoming === origin) return route.continue();
    refused.push(incoming);
    return route.abort();
  });
  const page = await context.newPage();
  await page.addInitScript(() => {
    const active = { MutationObserver: 0, ResizeObserver: 0 };
    for (const name of Object.keys(active)) {
      window[name] = class extends window[name] {
        #attached = false;
        observe(...args) {
          super.observe(...args);
          if (!this.#attached) active[name]++;
          this.#attached = true;
        }
        disconnect() {
          super.disconnect();
          if (this.#attached) active[name]--;
          this.#attached = false;
        }
      };
    }
    window.fixtureObservers = () => ({ ...active });
  });
  const errors = [];
  page.on('pageerror', error => errors.push(error.message));
  await page.goto(origin);
  await page.waitForFunction(() => typeof window.runSDKFixture === 'function', null, { timeout: 6000 });
  const result = await page.evaluate(() => window.runSDKFixture());
  assert.deepEqual(errors, []);
  assert.deepEqual(refused, []);
  assert.equal(result.items.length, 100);
  assert.ok(result.items.some(item => item.visibility === 'mounted_offscreen'));
  assert.equal(new Set(result.items.map(item => item.instance_ref.instance_id)).size, 100);
  assert.equal(result.exact37.status, 'exact_in_supported_scope');
  assert.deepEqual(result.exact37.targets[0].instance_ref, result.selected);
  assert.ok(result.items.some(item => item.instance_ref.instance_id === result.selected.instance_id));
  assert.equal(result.selected.hmr_epoch, 2);
  assert.equal(result.moved.targets[0].instance_ref.instance_id, result.selected.instance_id);
  assert.notDeepEqual(result.moved.targets[0].rects, result.exact37.targets[0].rects);
  assert.ok(result.moved.targets[0].rects[0].width > result.exact37.targets[0].rects[0].width);
  assert.equal(result.nested.status, 'exact_in_supported_scope');
  assert.deepEqual(result.nested.targets.map(target => target.anchor_ref.anchor_id),
    [result.anchors.host, result.anchors.root, result.anchors.outer]);
  assert.equal(result.group.rects.length, 2);
  assert.ok(result.group.rects[0].x + result.group.rects[0].width < result.group.rects[1].x);
  assert.equal(result.canvas.status, 'ancestor_fallback');
  assert.equal(result.opaque.status, 'unsupported');
  assert.equal(result.portal.status, 'unmapped');
  assert.equal(result.changesBeforeDetach, 2);
  assert.equal(result.staleObserved, true);
  assert.equal(result.staleRejected, true);
  assert.equal(result.boundaryUnmounted, true);
  assert.equal(result.afterDispose.status, 'unmapped');
  assert.ok(!result.afterDetach.items.some(item => item.instance_ref.instance_id === result.selected.instance_id));
  assert.equal(result.actions, 0);
  assert.equal(result.submits, 0);
  assert.equal(result.attachedObservers.MutationObserver - result.initialObservers.MutationObserver, 1);
  assert.equal(result.attachedObservers.ResizeObserver - result.initialObservers.ResizeObserver, 1);
  assert.deepEqual(result.finalObservers, result.initialObservers);
  const { build_map_id: _, ...identity } = result.identity;
  const message = { protocol_version: 1, ...identity, message_id: identity.session_id, reply_to: identity.session_id,
    seq: 1, selection_id: null, type: 'PICK_RESULT', payload: result.nested };
  assert.deepEqual(decodeBridgeMessage(JSON.stringify(message), 'sdk_to_parent', 1_000_000), message);
  console.log(JSON.stringify({ case: 'T086_DOM_REGISTRY', status: 'PASS', browser_sha256: browserDigest,
    instances: 100, selected: 37, separate_rects: 2, invalidated: true, actions: 0, submits: 0,
    leaked_sdk_observers: 0 }));
} finally {
  await browser?.close();
  server.close();
  server.closeAllConnections();
}
