import assert from 'node:assert/strict';
import { createHash, randomBytes } from 'node:crypto';
import { readFile, writeFile } from 'node:fs/promises';
import { createServer } from 'node:http';
import { fileURLToPath } from 'node:url';
import { build } from 'esbuild';
import { chromium } from 'playwright';
import { captureObservedArrival } from './explorer_observe_site.mjs';

const [profileID, fixtureBuild, fixtureRevision, output, mode] = process.argv.slice(2);
assert.match(profileID, /^[0-9a-f]{64}$/);
assert.match(fixtureBuild, /^[0-9a-f]{64}$/);
assert.match(fixtureRevision, /^[0-9a-f]{40}$/);
const browserPath = process.env.MCE4D_T086_CHROMIUM;
assert.equal(createHash('sha256').update(await readFile(browserPath)).digest('hex'),
  '24eb0a14de64ab0c9c2e283a5512ca817db64af8fd853cdb4d818f19a8e23b9b');

const repo = fileURLToPath(new URL('../../', import.meta.url));
async function bundle(entry) {
  const result = await build({ absWorkingDir: repo, entryPoints: [entry], bundle: true, format: 'esm', write: false });
  return result.outputFiles[0].text;
}
const [bridge, sdkPeer] = await Promise.all([
  bundle('web/explorer/src/bridge.ts'),
  bundle('tests/acceptance/explorer_observe_peer.mjs'),
]);
const sha = bytes => createHash('sha256').update(bytes).digest('hex');
const buildInputs = [sha(sdkPeer), sha(await readFile(new URL('./explorer_observe_site.mjs', import.meta.url))),
  sha(await readFile(new URL(import.meta.url)))];
const buildMapID = mode === 'verified' ? sha(JSON.stringify(buildInputs)) : fixtureBuild;
const revision = mode === 'verified' ? buildInputs[1] : fixtureRevision;
const live = [];
async function serve(handler) {
  const server = createServer(handler);
  await new Promise((resolve, reject) => server.once('error', reject).listen(0, '127.0.0.1', resolve));
  live.push(server);
  return 'http://127.0.0.1:' + server.address().port;
}
function send(res, mime, body, headers = {}) {
  res.writeHead(200, { 'content-type': mime, 'cache-control': 'no-store', ...headers });
  res.end(body);
}
const existingTraceParent = '00-' + '1'.repeat(32) + '-' + '2'.repeat(16) + '-01';
const actionNames = ['first', 'second', 'cors', 'redirect', 'outside', 'compatible', 'retry', 'cache', 'pending'];
let peer;
const seen = new Map();
const corsMethods = [];
let browser;
let firstResponse;
let pendingResponse;
let resolvePendingArrival;
const pendingArrival = new Promise(resolve => { resolvePendingArrival = resolve; });
const retries = [];
let refusedScriptReads = 0;
let backendSpan;
try {
  const base = await serve((req, res) => {
    if (req.url === '/bridge.js') send(res, 'text/javascript', bridge);
    else if (req.url === '/opener') send(res, 'text/html', '<!doctype html><a target="_blank" rel="opener" href="' +
      peer + '/coop">Open test app</a>');
    else send(res, 'text/html', '<!doctype html><iframe inert sandbox="allow-scripts allow-same-origin"></iframe>',
      { 'content-security-policy': "default-src 'self'; frame-src " + peer });
  });
  const cross = await serve((req, res) => {
    req.resume();
    corsMethods.push({ method: req.method, requestedTrace: req.headers['access-control-request-headers'] === 'traceparent' });
    // The test endpoint permits the origin and verb. It declines traceparent.
    res.writeHead(204, { 'access-control-allow-origin': peer, 'access-control-allow-methods': 'POST' });
    res.end();
  });
  peer = await serve((req, res) => {
    if (req.url === '/refused') {
      send(res, 'text/html', '<script src="/refused.js"></script>', { 'content-security-policy': "frame-ancestors 'none'" });
      return;
    }
    if (req.url === '/refused.js') {
      refusedScriptReads++;
      send(res, 'text/javascript', '');
      return;
    }
    if (req.url === '/coop') {
      send(res, 'text/html', '<!doctype html><title>Isolated test app</title>', { 'cross-origin-opener-policy': 'same-origin' });
      return;
    }
    if (req.url === '/peer.js') {
      send(res, 'text/javascript', sdkPeer);
      return;
    }
    if (req.url === '/app' || /^\/app\?reload=[0-2]$/.test(req.url)) {
      const options = { parentOrigin: base, corsOrigin: cross, existingTraceParent, approval: {
        environment: 'test', expiresAt: Date.now() + 60_000, buildMapID, profileID, revision,
        application: 'fixture', service: 'frontend', collector: 'fixture', instrumentation: 'v1',
        operations: ['request'], destinations: [{ origin: peer, pathPrefix: '/api', methods: ['POST'] },
          { origin: cross, pathPrefix: '/api', methods: ['POST'] }],
      } };
      const buttons = [...actionNames, 'arm', 'background', 'export'].map(name =>
        '<button type="button" id="' + name + '">' + name + '</button>').join('');
      const select = '<select id="export-action">' + actionNames.map(name => '<option>' + name + '</option>').join('') + '</select>';
      send(res, 'text/html', '<!doctype html><meta charset="utf-8">' + buttons + select + '<output id="export-output"></output>' +
        '<script id="options" type="application/json">' + JSON.stringify(options) + '</script><script type="module" src="/peer.js"></script>',
      { 'content-security-policy': "default-src 'none'; script-src 'self'; connect-src 'self' " + cross + '; frame-ancestors ' + base });
      return;
    }
    req.resume();
    const name = new URL(req.url, peer).pathname;
    if (req.method !== 'POST') {
      res.writeHead(404).end();
      return;
    }
    const received = req.headers.traceparent;
    const at = new Date().toISOString();
    seen.set(name, { traceparent: received, tracestate: req.headers.tracestate, at });
    assert.equal(req.headers.authorization, undefined);
    if (name === '/api/redirect') {
      res.writeHead(307, { location: '/api/redirected' }).end();
      return;
    }
    if (name === '/api/retry') {
      retries.push(received);
      res.writeHead(retries.length === 1 ? 503 : 204).end();
      return;
    }
    if (name === '/api/pending') {
      pendingResponse = res;
      resolvePendingArrival();
      return;
    }
    // The first request stays open until the second arrives.
    if (name === '/api/first') {
      backendSpan = captureObservedArrival(received, at, randomBytes(8).toString('hex'));
      firstResponse = res;
      return;
    }
    res.writeHead(204).end();
    if (name === '/api/second') {
      assert.ok(firstResponse);
      firstResponse.writeHead(204).end();
      firstResponse = undefined;
    }
  });

  browser = await chromium.launch({ executablePath: browserPath, chromiumSandbox: false, headless: true, timeout: 15_000 });
  const context = await browser.newContext({ serviceWorkers: 'block' });
  const page = await context.newPage();
  const origins = new Set();
  page.on('request', request => origins.add(new URL(request.url()).origin));
  const errors = [];
  page.on('pageerror', error => errors.push(error.message));
  await page.goto(base);
  const sessionID = await page.evaluate(async ({ peer, buildMapID, nonce }) => {
    const { decodeBridgeMessage } = await import('/bridge.js');
    const frame = document.querySelector('iframe');
    const sessionID = crypto.randomUUID();
    let sequence = 0;
    function command(type, payload, response) {
      const outgoing = { protocol_version: 1, session_id: sessionID, document_epoch: 1, hmr_epoch: 0, selection_id: null,
        seq: ++sequence, message_id: crypto.randomUUID(), reply_to: null, type, payload };
      let listener;
      const result = new Promise((resolve, reject) => {
        const deadline = setTimeout(() => {
          removeEventListener('message', listener);
          reject(new Error('Bridge response deadline: ' + type));
        }, 5000);
        listener = event => {
          if (event.origin !== peer || event.source !== frame.contentWindow) return;
          const message = decodeBridgeMessage(event.data, 'sdk_to_parent', 16_384);
          if (message.type !== response || message.reply_to !== outgoing.message_id || message.session_id !== sessionID) return;
          clearTimeout(deadline);
          removeEventListener('message', listener);
          resolve(message.payload);
        };
        addEventListener('message', listener);
        const wire = JSON.stringify(outgoing);
        decodeBridgeMessage(wire, 'parent_to_sdk', 16_384);
        frame.contentWindow.postMessage(wire, peer);
      });
      return result;
    }
    await new Promise(resolve => {
      frame.addEventListener('load', resolve, { once: true });
      frame.src = peer + '/app';
    });
    const ready = await command('CHALLENGE', { nonce, approved_origin: peer }, 'READY');
    if (ready.nonce !== nonce || ready.build_map_id !== buildMapID) throw new Error('Unexpected peer');
    await command('BOUND', { build_map_id: buildMapID, authorization_epoch: 0 }, 'BOUND_ACK');
    window.setObserveFixtureMode = async (mode, epoch) => {
      frame.inert = true;
      const ack = await command('SET_MODE', { mode, authorization_epoch: epoch, command_id: crypto.randomUUID() }, 'MODE_ACK');
      if (!ack.accepted || ack.authorization_epoch !== epoch || ack.mode !== mode) throw new Error('Mode rejected');
      if (mode === 'interact') frame.inert = false;
    };
    return sessionID;
  }, { peer, buildMapID, nonce: randomBytes(32).toString('base64url') });
  const target = page.frame({ url: peer + '/app' });
  assert.ok(target);
  page.setDefaultTimeout(10_000);
  const actionBounds = await target.locator('#first').boundingBox();
  assert.ok(actionBounds);
  await page.mouse.click(actionBounds.x + actionBounds.width / 2, actionBounds.y + actionBounds.height / 2);
  await target.locator('#arm').evaluate(button => button.click());
  assert.equal(await target.locator('html').getAttribute('data-armed'), null);
  assert.equal(seen.size, 0);
  await page.evaluate(() => window.setObserveFixtureMode('interact', 1));
  await target.locator('#first').click();
  assert.equal(seen.size, 0);
  await target.locator('#arm').evaluate(button => button.click());
  assert.equal(await target.locator('html').getAttribute('data-armed'), 'false');
  await target.locator('#arm').click();
  assert.equal(await target.locator('html').getAttribute('data-armed'), 'true');

  async function wait(name, status) {
    try {
      await target.waitForFunction(([key, value]) => document.documentElement.dataset[key] === value, [name, status]);
    } catch (error) {
      const actual = await target.locator('html').getAttribute('data-' + name);
      throw new Error(`Fixture action ${name} reported ${actual}; expected ${status}`, { cause: error });
    }
  }
  async function localExport(name) {
    await target.locator('#export-action').selectOption(name);
    await target.locator('#export').click();
    const value = await target.locator('#export-output').textContent();
    return value ? JSON.parse(value) : null;
  }
  await target.locator('#first').click();
  await target.locator('#second').click();
  await target.locator('#background').click();
  await wait('second', 'ok');
  await wait('background', 'ok');
  await wait('first', 'ok');
  const one = await localExport('first');
  const two = await localExport('second');
  assert.equal(await localExport('first'), null);
  function retainedProof(exported, path) {
    assert.equal(exported.receipts.length, 1);
    assert.equal(exported.artifact.spans.length, 1);
    const receipt = exported.receipts[0];
    const span = exported.artifact.spans[0];
    assert.equal(receipt.traceparent, seen.get(path).traceparent);
    assert.equal(receipt.interaction_id, span.interaction_id);
    assert.equal(receipt.trace_id, span.trace_id);
    assert.equal(receipt.span_id, span.span_id);
    assert.equal(receipt.session_id, sessionID);
    assert.equal(receipt.authorization_epoch, 1);
    assert.equal(receipt.build_map_id, buildMapID);
    assert.doesNotMatch(JSON.stringify(exported), /fixture-query|synthetic-body|synthetic-secret-marker|x-fixture-debug|127\.0\.0\.1|\/api\/|tracestate/i);
    return receipt;
  }
  const first = retainedProof(one, '/api/first');
  const second = retainedProof(two, '/api/second');
  for (const key of ['interaction_id', 'request_id', 'trace_id', 'span_id']) assert.notEqual(first[key], second[key]);
  assert.equal(seen.get('/api/background').traceparent, undefined);
  for (const action of ['outside', 'compatible']) {
    await target.locator('#' + action).click();
    await wait(action, 'ok');
    assert.equal(await localExport(action), null);
  }
  assert.equal(seen.get('/apiv2').traceparent, undefined);
  assert.equal(seen.get('/api/compatible').traceparent, existingTraceParent);
  assert.equal(seen.get('/api/compatible').tracestate, 'fixture=sampled');
  for (const action of ['cors', 'redirect']) {
    await target.locator('#' + action).click();
    await wait(action, 'Transport unavailable');
    const failed = await localExport(action);
    assert.equal(failed.artifact.spans.length, 1);
    assert.equal(failed.artifact.spans[0].status, 'error');
  }
  assert.deepEqual(corsMethods, [{ method: 'OPTIONS', requestedTrace: true }]);
  assert.equal(seen.has('/api/redirected'), false);
  await page.evaluate(() => window.setObserveFixtureMode('inspect', 2));
  await page.evaluate(() => window.setObserveFixtureMode('interact', 3));
  await target.locator('#export').click();
  assert.equal(await target.locator('#export-output').textContent(), '');
  if (mode === 'faults') {
    const scenarioStarted = performance.now();
    await target.locator('#arm').click();
    assert.equal(await target.evaluate(() => window.observeFixtureArmed()), true);
    await target.locator('#retry').click();
    await wait('retry', 'ok');
    const attempts = await localExport('retry');
    assert.deepEqual(attempts.artifact.spans.map(span => span.status), ['error', 'ok']);
    assert.deepEqual(attempts.receipts.map(receipt => receipt.traceparent), retries);
    assert.equal(attempts.receipts[0].interaction_id, attempts.receipts[1].interaction_id);
    assert.notEqual(attempts.receipts[0].request_id, attempts.receipts[1].request_id);
    assert.notEqual(attempts.receipts[0].span_id, attempts.receipts[1].span_id);
    const requestsBeforeCacheHit = seen.size;
    await target.locator('#cache').click();
    await wait('cache', 'ok');
    assert.equal(await localExport('cache'), null);
    assert.equal(seen.size, requestsBeforeCacheHit);
    await target.locator('#pending').click();
    await pendingArrival;
    assert.equal(await localExport('pending'), null);
    assert.equal(await target.evaluate(() => window.changeFixtureBuild('f'.repeat(64))), true);
    assert.equal(await target.evaluate(() => window.observeFixtureArmed()), false);
    pendingResponse.writeHead(204).end();
    pendingResponse = undefined;
    await wait('pending', 'ok');
    assert.equal(await target.evaluate(() => window.observeFixtureArmed()), false);
    for (let i = 0; i < 3; i++) {
      await page.evaluate(src => new Promise(resolve => {
        const iframe = document.querySelector('iframe');
        iframe.inert = true;
        iframe.addEventListener('load', resolve, { once: true });
        iframe.src = src;
      }), peer + '/app?reload=' + i);
      const replacement = page.frame({ url: peer + '/app?reload=' + i });
      assert.ok(replacement);
      assert.equal(await replacement.evaluate(() => window.observeFixtureArmed()), false);
    }
    await page.evaluate(src => new Promise(resolve => {
      const iframe = document.querySelector('iframe');
      iframe.addEventListener('load', resolve, { once: true });
      iframe.src = src;
    }), peer + '/refused');
    assert.equal(refusedScriptReads, 0);
    assert.equal(page.frames().some(frame => frame.url() === peer + '/refused'), false);
    await page.goto(base + '/opener');
    const notifyPopup = context.waitForEvent('page');
    await page.getByRole('link', { name: 'Open test app' }).click();
    const popup = await notifyPopup;
    await popup.waitForURL(peer + '/coop');
    assert.equal(await popup.evaluate(() => window.opener === null), true);
    await popup.close();
    console.log(JSON.stringify({ check: 'T103_BROWSER_OBSERVE_FAULTS', status: 'PASS', retries: retries.length,
      cachedActionUnbound: true, pendingBuildInvalidation: true, cleanReloads: 3, cspDenied: true, coopOpenerAbsent: true,
      browser: browser.version(), elapsed_ms: Math.round(performance.now() - scenarioStarted) }));
  }
  assert.deepEqual(errors, []);
  assert.ok([...origins].every(origin => [base, peer, cross].includes(origin)));

  const arrival = seen.get('/api/first').at;
  assert.equal(backendSpan.trace_id, first.trace_id);
  assert.equal(backendSpan.parent_span_id, first.span_id);
  const backend = { metadata: { ...one.artifact.metadata, service: 'backend', window_start: arrival, window_end: arrival }, spans: [backendSpan] };
  if (mode === 'verified') await writeFile(output + '.bundle.mjs', sdkPeer, { mode: 0o600, flag: 'wx' });
  await writeFile(output, JSON.stringify({ first: one, second: two, backend, childID: backendSpan.span_id, buildInputs }) + '\n',
    { mode: 0o600, flag: 'wx' });
  console.log(JSON.stringify({ check: 'T102_BROWSER_CAUSAL_EXPORT', status: 'PASS', browser: browser.version(),
    trustedActions: 2, unrelatedBackgroundRequests: 1, existingCarriersPreserved: true,
    preflightDenied: true, propagatedRedirectDenied: true, inspectActionRequests: 0, exportsMetadataOnly: true }));
} finally {
  firstResponse?.destroy();
  pendingResponse?.destroy();
  await browser?.close();
  await Promise.all(live.map(server => new Promise(resolve => server.close(resolve))));
}
