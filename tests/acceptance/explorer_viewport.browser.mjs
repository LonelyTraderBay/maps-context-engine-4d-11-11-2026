import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { readFile } from 'node:fs/promises';
import { chromium } from 'playwright';

let input = '';
for await (const chunk of process.stdin) {
  input += chunk;
  assert.ok(input.length <= 16384);
}
const fixture = JSON.parse(input);
const base = new URL(fixture.origin);
assert.equal(base.hostname, '127.0.0.1');
const chrome = process.env.MCE4D_T083_CHROMIUM;
assert.equal(createHash('sha256').update(await readFile(chrome)).digest('hex'),
  '24eb0a14de64ab0c9c2e283a5512ca817db64af8fd853cdb4d818f19a8e23b9b');
const browser = await chromium.launch({ executablePath: chrome, headless: true, chromiumSandbox: false });

function percentiles(samples) {
  const ordered = samples.toSorted((a, b) => a - b);
  return { samples_ms: samples.map(Math.round), p95_ms: Math.round(ordered[Math.ceil(samples.length * .95) - 1]) };
}

try {
  const context = await browser.newContext({ viewport: { width: 1280, height: 820 }, reducedMotion: 'reduce',
    serviceWorkers: 'block', acceptDownloads: false });
  const requests = [];
  const outside = [];
  let holdSource = null;
  await context.route('**/*', async route => {
    const request = route.request();
    const url = new URL(request.url());
    if (url.origin !== base.origin) {
      outside.push(url.origin);
      await route.abort();
      return;
    }
    requests.push(url.pathname);
    if (holdSource && url.pathname.endsWith('/source/read')) {
      const waiting = holdSource;
      holdSource = null;
      waiting.arrived();
      await waiting.release;
      await route.continue().catch(() => {});
      return;
    }
    await route.continue();
  });
  await context.addInitScript(() => {
    const NativeWorker = window.Worker;
    window.layoutAudit = { started: 0, terminated: 0, slow: true };
    window.Worker = class extends NativeWorker {
      constructor(...args) {
        super(...args);
        window.layoutAudit.started++;
      }
      postMessage(input, ...rest) {
        if (window.layoutAudit.slow) this.delay = setTimeout(() => super.postMessage(input, ...rest), 2000);
        else super.postMessage(input, ...rest);
      }
      terminate() {
        clearTimeout(this.delay);
        if (!this.stopped) window.layoutAudit.terminated++;
        this.stopped = true;
        super.terminate();
      }
    };
  });
  const page = await context.newPage();
  page.setDefaultTimeout(12000);
  const graph = page.locator('#graph-pane');
  const ready = page.getByRole('button', { name: 'Tải lại', exact: true });
  const canvas = graph.locator('.fact-node').first();
  const readGraph = () => page.waitForResponse(reply => reply.url().endsWith('/graph/query') &&
    reply.request().postDataJSON()?.root?.kind === 'workspace' && !reply.request().postDataJSON()?.cursor);
  const readSource = () => page.waitForResponse(reply => reply.url().endsWith('/source/read'));
  const source = page.locator('#source-pane').getByLabel('Mã nguồn của snapshot');
  const row = name => graph.locator('.symbol-row').filter({ has: page.getByText(name, { exact: true }) });
  const cold = performance.now();
  await page.goto(base.href);
  await page.getByLabel('Mã bootstrap').fill(fixture.code);
  const initial = readGraph();
  await page.getByRole('button', { name: 'Kết nối', exact: true }).click();
  const reply = await initial;
  assert.equal(reply.status(), 200);
  const data = await reply.json();
  assert.equal(data.status, 'partial');
  assert.equal(data.pagination.truncated, true);
  assert.equal(data.pagination.enumeration_complete, false);
  assert.equal(data.pagination.required_pending, null);
  assert.equal(typeof data.pagination.next_cursor, 'string');
  assert.ok(data.coverage.gaps.some(gap => gap.code === 'GRAPH_PROJECTION_LIMIT'));
  assert.ok(data.data.nodes.length > 2 && data.data.nodes.length <= 100);
  assert.deepEqual(data.data.edges, []);
  const coldResponseMS = performance.now() - cold;
  await page.waitForFunction(() => window.layoutAudit.started > window.layoutAudit.terminated);
  await graph.getByRole('button', { name: 'Danh sách', exact: true }).click();
  await page.waitForFunction(() => window.layoutAudit.started === window.layoutAudit.terminated);
  await page.evaluate(() => { window.layoutAudit.slow = false; });
  await graph.getByRole('button', { name: 'Bản đồ', exact: true }).click();
  await canvas.waitFor();

  const graphTimes = [];
  for (let i = 0; i < 20; i++) {
    const next = readGraph();
    await ready.click();
    const sample = await next;
    assert.equal(sample.status(), 200);
    await canvas.waitFor();
    const rendered = await page.evaluate(() => performance.timeOrigin + performance.now());
    graphTimes.push(rendered - sample.request().timing().startTime);
    assert.ok(await graph.locator('.fact-node').count() <= 200);
  }
  const first = data.data.nodes.filter(node => /^Viewport\d{6}$/.test(node.label)).slice(0, 2);
  assert.equal(first.length, 2);
  const oracle = ({ label }) => label;
  for (const { label, source_refs: [ref] } of first) {
    const part = label.slice(8, 10);
    const number = Number(label.slice(-4));
    let start = 'package demo\n'.length + 'func '.length;
    for (let i = 0; i < number; i++) {
      start += `func Viewport${part}${String(i).padStart(4, '0')}() int { return ${i} }\n`.length;
    }
    assert.equal(ref.path, `viewport_${part}.go`);
    assert.deepEqual(ref.range, { start_byte: start, end_byte: start + label.length });
  }

  await graph.getByRole('button', { name: 'Danh sách', exact: true }).click();
  const filter = graph.getByRole('searchbox', { name: 'Lọc ký hiệu' });
  const group = graph.getByRole('button', { name: 'Nhóm theo tệp' });
  await filter.fill(first[0].label);
  await group.click();
  const summary = graph.locator('summary').filter({ hasText: first[0].source_refs[0].path }).last();
  await summary.focus();
  await summary.press('Enter');
  assert.equal(await row(first[0].label).isVisible(), true);
  await row(first[0].label).press('Enter');
  await source.getByText(oracle(first[0]), { exact: true }).waitFor();
  await summary.press('Enter');
  await source.getByText(oracle(first[0]), { exact: true }).waitFor();
  await summary.press('Enter');
  assert.equal(await row(first[0].label).getAttribute('aria-pressed'), 'true');
  await group.click();
  await filter.fill('');
  const moreRows = graph.getByRole('button', { name: /^Hiện thêm ký hiệu/ });
  while (await moreRows.isVisible()) await moreRows.click();
  const headers = await reply.request().allHeaders();
  const mismatched = await page.request.post(reply.url(), { headers: {
    Authorization: headers.authorization, Origin: base.origin, 'X-MCE4D-Intent': 'explorer',
  }, data: { ...reply.request().postDataJSON(), cursor: data.pagination.next_cursor, kinds: ['CALLS_DIRECT'] } });
  assert.equal((await mismatched.json()).error?.code, 'CURSOR_INVALID');

  await page.waitForFunction(() => Array.from(document.querySelectorAll('button'))
    .some(button => button.textContent === 'Tải lại' && !button.disabled));
  const startedWorkers = await page.evaluate(() => window.layoutAudit.started);
  const requestCount = requests.length;
  await page.evaluate(async () => {
    const target = document.querySelector('.symbol-row');
    for (let frame = 0; frame < 60; frame++) {
      await new Promise(resolve => requestAnimationFrame(() => resolve()));
      target.dispatchEvent(new PointerEvent('pointermove', { bubbles: true, clientX: frame, clientY: 20 }));
    }
  });
  assert.deepEqual(requests.slice(requestCount).filter(path => path.startsWith('/api/explorer/')), []);
  assert.equal(await page.evaluate(() => window.layoutAudit.started), startedWorkers);

  const sourceTimes = [];
  for (let i = 0; i < 20; i++) {
    const symbol = first[(i + 1) % 2];
    const pending = readSource();
    const start = performance.now();
    await row(symbol.label).click();
    const response = await pending;
    assert.equal(response.status(), 200);
    assert.deepEqual((await response.json()).data.source_ref, symbol.source_refs[0]);
    await source.getByText(oracle(symbol), { exact: true }).waitFor();
    sourceTimes.push(performance.now() - start);
  }
  const arrived = Promise.withResolvers();
  const held = Promise.withResolvers();
  holdSource = { arrived: arrived.resolve, release: held.promise };
  try {
    await row(first[1].label).click();
    await arrived.promise;
    await row(first[0].label).click();
    await source.getByText(oracle(first[0]), { exact: true }).waitFor();
  } finally {
    held.resolve();
  }
  assert.equal(await row(first[0].label).getAttribute('aria-pressed'), 'true');
  assert.equal(await source.textContent(), oracle(first[0]));
  assert.deepEqual(outside, []);
  const graphLatency = percentiles(graphTimes);
  const sourceLatency = percentiles(sourceTimes);
  const status = graphLatency.p95_ms <= 1500 && sourceLatency.p95_ms <= 500 ? 'PASS' : 'FAIL';
  console.log(JSON.stringify({ case: 'T099_NATIVE_VIEWPORT', status, cold_response_ms: Math.round(coldResponseMS),
    graph_warm: graphLatency, source_warm: sourceLatency, visible_nodes: data.data.nodes.length,
    bounded_partial: true, worker_cancelled: true, hover_frames: 60, hover_rpc: 0, stale_source_applied: false,
    outside_requests: outside.length, original_source: true }));
  assert.equal(status, 'PASS');
} finally {
  await browser.close();
}
