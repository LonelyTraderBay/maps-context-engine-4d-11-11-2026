import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { createHash } from 'node:crypto';
import { once } from 'node:events';
import { createServer } from 'node:http';
import { join } from 'node:path';
import { chromium } from 'playwright';

const scenario = process.argv[2];
assert.ok(['empty', 'indexed', 'benchmark'].includes(scenario));
let inputText = '';
process.stdin.setEncoding('utf8');
for await (const chunk of process.stdin) {
  inputText += chunk;
  assert.ok(inputText.length < 16384);
}
const input = JSON.parse(inputText);
const base = new URL(input.origin);
assert.equal(base.hostname, '127.0.0.1');
assert.equal(base.protocol, 'http:');
assert.match(input.code, /^[A-Za-z0-9_-]{43}$/);
const browserPath = process.env.MCE4D_T083_CHROMIUM;
const expectedBrowser = '24eb0a14de64ab0c9c2e283a5512ca817db64af8fd853cdb4d818f19a8e23b9b';
const browserSHA = createHash('sha256').update(await readFile(browserPath)).digest('hex');
assert.equal(browserSHA, expectedBrowser);
const evidence = process.env.MCE4D_T083_EVIDENCE;
const browser = await chromium.launch({ executablePath: browserPath, headless: true,
  // The fixture broker already isolates PID, mounts and network. Chromium is
  // an unprivileged child on WSL where the nested sandbox may be unavailable.
  chromiumSandbox: false, timeout: 15000 });

async function verifyFrames(context, allowPeer) {
  const peer = createServer((_request, response) => {
    response.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8' });
    response.end(`<!doctype html><title>GU0 local frame fixture</title>
      <iframe id="preview" sandbox="allow-scripts" srcdoc="<p>Local frame fixture</p>"></iframe>
      <iframe id="embed" src="${base.href}"></iframe>`);
  });
  peer.listen(0, '127.0.0.1');
  try {
    await once(peer, 'listening');
    const origin = 'http://127.0.0.1:' + peer.address().port;
    allowPeer(origin);
    const page = await context.newPage();
    try {
      await page.goto(origin, { timeout: 10000 });
      const observer = await page.locator('#preview').elementHandle().then(element => element.contentFrame());
      assert.ok(observer);
      assert.equal(await observer.evaluate(() => self.origin), 'null');
      const readable = await observer.evaluate(async url => {
        try {
          await fetch(url, { method: 'POST', body: '{}' });
          return true;
        } catch {
          return false;
        }
      }, new URL('/api/explorer/v1/source/read', base).href);
      assert.equal(readable, false);
      const embedded = await page.locator('#embed').elementHandle().then(element => element.contentFrame());
      assert.ok(embedded && embedded.url().startsWith('chrome-error:'), 'Explorer can be loaded only as a top-level page');
      console.log(JSON.stringify({ check: 'C083_BROWSER', status: 'PASS', opaque_frame_source_readable: false,
        explorer_iframe_blocked: true }));
    } finally {
      await page.close();
    }
  } finally {
    peer.close();
    peer.closeAllConnections();
  }
}

function summarize(samples) {
  const ordered = samples.slice().sort((left, right) => left - right);
  return { samples_ms: samples.map(value => Math.round(value)),
    p95_ms: Math.round(ordered[Math.ceil(ordered.length * 0.95) - 1]) };
}

async function benchmarkGraphAndSource(page, expected, coldStart) {
  const graph = page.locator('#graph-pane');
  const row = (name) => graph.getByRole('button', { name: new RegExp('^Chọn ' + name + ',') });
  await row('CanonicalNull').waitFor();
  const coldInitialMS = performance.now() - coldStart;
  const graphTimes = [];
  const reloadTimes = [];
  const sourceTimes = [];
  const retry = page.getByRole('button', { name: 'Tải lại', exact: true });
  const readyToReload = () => page.waitForFunction(() => {
    const reload = Array.from(document.querySelectorAll('button')).find(button => button.textContent === 'Tải lại');
    return reload && !reload.disabled;
  }, undefined, { polling: 16 });
  for (let i = 0; i < 20; i++) {
    await readyToReload();
    const graphResponse = page.waitForResponse(reply => {
      if (!reply.url().endsWith('/graph/query')) return false;
      const request = reply.request().postDataJSON();
      return request.root.kind === 'workspace' && !request.cursor;
    });
    const start = performance.now();
    await retry.click();
    const reply = await graphResponse;
    assert.equal(reply.status(), 200);
    const result = await reply.json();
    assert.ok(result.data.nodes.length <= 200);
    const firstTitles = result.data.nodes.map(node => node.label).filter(Boolean);
    const marker = await page.waitForFunction(titles => {
      const rows = document.querySelectorAll('#graph-pane .fact-node__label');
      return Array.from(rows).some(row => titles.includes(row.textContent)) ?
        performance.timeOrigin + performance.now() : 0;
    }, firstTitles, { polling: 16 });
    const painted = await marker.jsonValue();
    const sliceMS = painted - reply.request().timing().startTime;
    assert.ok(sliceMS > 0);
    graphTimes.push(sliceMS);
    await row('CanonicalNull').waitFor();
    reloadTimes.push(performance.now() - start);
    assert.equal(await page.locator('iframe').count(), 0);
  }
  await readyToReload();
  const source = page.locator('#source-pane');
  for (let i = 0; i < 20; i++) {
    const name = i % 2 === 0 ? 'CanonicalNull' : 'CanonicalBool';
    const oracle = expected[name];
    const pending = page.waitForResponse(reply => reply.url().endsWith('/source/read'));
    const start = performance.now();
    await row(name).click();
    const reply = await pending;
    assert.equal(reply.status(), 200);
    const result = (await reply.json()).data;
    assert.equal(result.source_ref.path, oracle.path);
    assert.equal(result.source_ref.content_hash, oracle.hash);
    assert.deepEqual(result.source_ref.range, { start_byte: oracle.start, end_byte: oracle.end });
    assert.equal(result.content, oracle.text);
    await source.getByText(oracle.text, { exact: true }).last().waitFor();
    await page.waitForFunction(text => document.querySelector('.source-code')?.textContent === text, oracle.text,
      { polling: 16 });
    sourceTimes.push(performance.now() - start);
  }
  const graphLatency = summarize(graphTimes);
  const sourceLatency = summarize(sourceTimes);
  const status = graphLatency.p95_ms <= 1500 ? 'PASS' : 'FAIL';
  console.log(JSON.stringify({ check: 'T094_REAL_GRAPH_SOURCE', status, cold_known_node_ms: Math.round(coldInitialMS),
    graph_warm: graphLatency, graph_reload_warm: summarize(reloadTimes), source_warm: sourceLatency,
    correct_source: sourceTimes.length, source_attempts: 20,
    oracle: expected, live_dev: 'NOT_RUN' }));
  assert.equal(status, 'PASS', 'PLAN-UI-03 graph-only p95');
}

try {
  const context = await browser.newContext({
    viewport: { width: 1440, height: 880 }, reducedMotion: 'reduce',
    serviceWorkers: 'block', acceptDownloads: false,
  });
  const unexpected = [];
  let peerOrigin = null;
  await context.route('**/*', route => {
    const origin = new URL(route.request().url()).origin;
    if (origin !== base.origin && origin !== peerOrigin) {
      unexpected.push(origin);
      return route.abort();
    }
    return route.continue();
  });
  const page = await context.newPage();
  page.setDefaultTimeout(16000);
  const errors = [];
  page.on('pageerror', error => errors.push(error.message));
  const requests = { workspace: 0, graph: 0, source: 0 };
  page.on('request', request => {
    const path = new URL(request.url()).pathname;
    if (path.endsWith('/workspace')) requests.workspace++;
    if (path.endsWith('/graph/query')) requests.graph++;
    if (path.endsWith('/source/read')) requests.source++;
  });
  const coldStart = performance.now();
  const appDocument = await page.goto(base.href, { timeout: 10000 });
  if (input.gu0) {
    const headers = appDocument.headers();
    assert.match(headers['content-security-policy'], /(?:^|; )script-src 'self';/);
    assert.match(headers['content-security-policy'], /(?:^|; )connect-src 'self';/);
    assert.match(headers['content-security-policy'], /(?:^|; )frame-ancestors 'none';/);
    assert.equal(headers['x-frame-options'], 'DENY');
    assert.equal(headers['access-control-allow-origin'], undefined);
  }
  await page.getByLabel('Mã bootstrap').fill(input.code);
  await page.getByRole('button', { name: 'Kết nối', exact: true }).click();
  await page.getByRole('heading', { name: 'Đồ thị', exact: true }).waitFor();

  if (scenario === 'empty') {
    await page.getByText('Workspace chưa có index', { exact: true }).waitFor();
    assert.equal(requests.graph, 0);
    assert.equal(requests.source, 0);
    assert.equal(await page.locator('.react-flow__node').count(), 0);
    assert.equal(await page.locator('.source-code').count(), 0);
  } else if (scenario === 'benchmark') {
    await benchmarkGraphAndSource(page, input.expected, coldStart);
  } else {
    const graph = page.locator('#graph-pane');
    const source = page.locator('#source-pane');
    const splitter = page.getByRole('separator', { name: 'Độ rộng đồ thị' });
    const left = await graph.boundingBox();
    const right = await source.boundingBox();
    assert.ok(left && right && left.x + left.width <= right.x);
    assert.equal(await splitter.getAttribute('aria-valuenow'), '45');
    await graph.getByText('Index chỉ bao phủ một phần.', { exact: false }).waitFor();
    await graph.locator('.react-flow__edge').first().waitFor({ state: 'attached' });
    const canvasCount = await graph.locator('.react-flow__node').count();
    const edgeCount = await graph.locator('.react-flow__edge').count();
    assert.ok(canvasCount >= 5 && edgeCount > 0, 'render typed facts and real edges');
    const code = source.locator('.source-code');
    async function inspect(symbol) {
      const expected = input.expected[symbol];
      await source.getByRole('heading', { name: symbol, exact: true }).waitFor();
      await code.waitFor();
      await page.waitForFunction(({ text }) => document.querySelector('.source-code')?.textContent === text, expected);
      assert.equal(await code.textContent(), expected.text);
      assert.equal(await source.locator('.file-path').textContent(), expected.path);
      assert.equal(await source.locator('.digest').getAttribute('title'), expected.hash);
      await source.getByText(`Byte [${expected.start}, ${expected.end})`, { exact: true }).waitFor();
      assert.equal(await code.getAttribute('tabindex'), '0');
    }
    const show = graph.getByRole('button', { name: /^Chọn Show,/ });
    await show.press('Enter');
    await inspect('Show');
    const renderedBeforePaging = requests.source;
    const more = graph.getByRole('button', { name: 'Tải trang tiếp', exact: true });
    for (let pageIndex = 0; pageIndex < 5 && await more.count(); pageIndex++) {
      const continued = page.waitForResponse(reply => reply.url().endsWith('/graph/query'));
      await more.click();
      assert.equal((await continued).status(), 200);
      await page.waitForFunction(() => {
        const button = Array.from(document.querySelectorAll('button')).find(item => item.textContent === 'Tải trang tiếp');
        return !button || !button.disabled;
      });
    }
    assert.equal(await more.count(), 0);
    const allEdges = await graph.locator('.react-flow__edge').count();
    assert.ok(allEdges > edgeCount);
    await inspect('Show');
    assert.equal(requests.source, renderedBeforePaging);
    await source.getByRole('button', { name: 'Chi tiết', exact: true }).press('Enter');
    await source.getByRole('heading', { name: 'Quan hệ trong lát đồ thị' }).waitFor();
    await source.getByRole('button', { name: 'Mã nguồn', exact: true }).press('Enter');
    await inspect('Show');
    const countBeforeResize = requests.source;
    await splitter.focus();
    await splitter.press('Home');
    assert.equal(await splitter.getAttribute('aria-valuenow'), '30');
    await splitter.press('End');
    assert.equal(await splitter.getAttribute('aria-valuenow'), '70');
    await splitter.press('ArrowLeft');
    assert.equal(await splitter.getAttribute('aria-valuenow'), '68');
    const handle = await splitter.boundingBox();
    assert.ok(handle);
    await page.mouse.move(handle.x + handle.width / 2, handle.y + handle.height / 2);
    await page.mouse.down();
    await page.mouse.move(864, handle.y + handle.height / 2, { steps: 6 });
    await page.mouse.up();
    assert.equal(await splitter.getAttribute('aria-valuenow'), '60');
    await inspect('Show');
    assert.equal(requests.source, countBeforeResize);
    await graph.getByRole('button', { name: 'Vừa khung', exact: true }).click();
    await page.screenshot({ path: join(evidence, 'explorer-desktop.png') });
    await graph.getByRole('button', { name: 'Danh sách', exact: true }).press('Enter');
    const list = graph.getByRole('list', { name: 'Ký hiệu trong đồ thị' });
    const read = list.getByRole('button', { name: /^Read\s/ });
    await read.press('Enter');
    await inspect('Read');
    assert.equal(await read.getAttribute('aria-pressed'), 'true');
    await page.getByRole('button', { name: 'Lựa chọn trước' }).press('Enter');
    await inspect('Show');
    await page.getByRole('button', { name: 'Lựa chọn sau' }).press('Enter');
    await inspect('Read');
    await page.setViewportSize({ width: 420, height: 860 });
    const switcher = page.getByRole('navigation', { name: 'Chọn khung' });
    await switcher.getByRole('button', { name: 'Nguồn', exact: true }).press('Enter');
    await inspect('Read');
    assert.equal(await graph.isVisible(), false);
    assert.equal(await splitter.isVisible(), false);
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true);
    await page.screenshot({ path: join(evidence, 'explorer-compact.png') });
    await switcher.getByRole('button', { name: 'Đồ thị', exact: true }).press('Enter');
    assert.equal(await read.getAttribute('aria-pressed'), 'true');
    await read.press('Escape');
    assert.equal(await read.getAttribute('aria-pressed'), 'false');
    if (input.gu0) {
      assert.equal(await page.locator('iframe').count(), 0);
      await verifyFrames(context, origin => { peerOrigin = origin; });
      await page.bringToFront();
      await page.setViewportSize({ width: 1440, height: 880 });
      const graphTimes = [];
      const reloadTimes = [];
      const sourceTimes = [];
      const retry = page.getByRole('button', { name: 'Tải lại', exact: true });
      for (let sample = 0; sample < 20; sample++) {
        const start = performance.now();
        const first = page.waitForResponse(reply => reply.url().endsWith('/graph/query'));
        await retry.click();
        const reply = await first;
        assert.equal(reply.status(), 200);
        await page.waitForFunction(() => Boolean(document.querySelector('.symbol-list button')),
          undefined, { polling: 16 });
        const displayed = await page.evaluate(() => performance.timeOrigin + performance.now());
        const sliceTime = displayed - reply.request().timing().startTime;
        assert.ok(sliceTime > 0);
        graphTimes.push(sliceTime);
        await page.waitForFunction(() => {
          const reload = Array.from(document.querySelectorAll('button')).find(button => button.textContent === 'Tải lại');
          return reload && !reload.disabled && document.querySelector('.symbol-list button');
        }, undefined, { polling: 16 });
        reloadTimes.push(performance.now() - start);
      }
      for (let sample = 0; sample < 20; sample++) {
        const symbol = sample % 2 === 0 ? 'Show' : 'Read';
        const start = performance.now();
        await list.getByRole('button', { name: new RegExp('^' + symbol + '\\s') }).press('Enter');
        await page.waitForFunction(expected => document.querySelector('.source-code')?.textContent === expected,
          symbol, { polling: 16 });
        sourceTimes.push(performance.now() - start);
      }
      await read.press('Escape');
      const graphLatency = summarize(graphTimes);
      const sourceLatency = summarize(sourceTimes);
      const status = graphLatency.p95_ms <= 1500 && sourceLatency.p95_ms <= 500 ? 'PASS' : 'FAIL';
      console.log(JSON.stringify({ check: 'GU0_WARM_LATENCY', status, graph: graphLatency,
        source: sourceLatency, reload: summarize(reloadTimes) }));
      assert.equal(status, 'PASS', 'GU0 fixture latency targets');
    }
    await context.setOffline(true);
    await page.getByRole('button', { name: 'Tải lại', exact: true }).click();
    await graph.getByRole('alert').waitFor();
    assert.equal(await page.evaluate(() => localStorage.length + sessionStorage.length), 0);
    assert.equal(await code.count(), 0);
    console.log(JSON.stringify({ check: 'C081', nodes_rendered: canvasCount, edges_rendered: allEdges,
      keyboard_split_bounds: [30, 70], pointer_split: 60,
      selection: 'canvas and semantic list by Enter, history, resize, compact, Escape',
      reduced_motion: await page.evaluate(() => matchMedia('(prefers-reduced-motion: reduce)').matches),
    }));
  }
  assert.deepEqual(errors, []);
  assert.deepEqual(unexpected, []);
  console.log(JSON.stringify({ check: 'C082', scenario, status: 'PASS', chromium: browser.version(),
    browser_sha256: browserSHA, requests, proof: scenario === 'empty' ? 'no graph or source requests' : 'pinned index and raw source oracle',
  }));
} finally {
  await browser.close();
}
