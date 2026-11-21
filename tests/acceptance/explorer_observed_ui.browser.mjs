import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { readFile } from 'node:fs/promises';
import { chromium } from 'playwright';

let input = '';
for await (const chunk of process.stdin) {
  input += chunk;
  assert.ok(input.length < 8192);
}
const fixture = JSON.parse(input);
const origin = new URL(fixture.origin);
assert.equal(origin.hostname, '127.0.0.1');
const executablePath = process.env.MCE4D_T086_CHROMIUM;
assert.equal(createHash('sha256').update(await readFile(executablePath)).digest('hex'),
  '24eb0a14de64ab0c9c2e283a5512ca817db64af8fd853cdb4d818f19a8e23b9b');

const browser = await chromium.launch({ executablePath, headless: true, chromiumSandbox: false, timeout: 15_000 });
try {
  const context = await browser.newContext({ viewport: { width: 1280, height: 820 }, serviceWorkers: 'block' });
  const page = await context.newPage();
  page.setDefaultTimeout(10_000);
  const outside = [];
  const paths = [];
  page.on('request', request => {
    const url = new URL(request.url());
    if (url.origin !== origin.origin) outside.push(url.origin);
    paths.push(url.pathname);
  });
  await page.goto(origin.href);
  await page.getByLabel('Mã bootstrap').fill(fixture.code);
  const graphResponse = page.waitForResponse(response => response.url().endsWith('/graph/query'));
  const initial = performance.now();
  await page.getByRole('button', { name: 'Kết nối', exact: true }).click();
  const graph = await graphResponse;
  const indexed = await graph.json();
  assert.equal(graph.status(), 200, indexed.error?.code);
  assert.ok(indexed.data.nodes.some(node => node.ref.symbol_id === fixture.symbol),
    JSON.stringify(indexed.data.nodes.map(node => node.label)));
  const choose = page.locator('#graph-pane').getByRole('button', { name: new RegExp('^Chọn ' + fixture.symbol + ',') });
  await choose.waitFor();
  const graphMS = performance.now() - initial;
  await choose.click();
  await page.getByRole('button', { name: 'Flow backend', exact: true }).click();
  const pane = page.getByRole('region', { name: 'Flow quan sát' });
  const id = page.getByLabel('Mã trace đã nhập');
  await id.fill(fixture.artifactID);
  const displayMS = [];
  for (let i = 0; i < 12; i++) {
    const flowResponse = page.waitForResponse(response => response.url().endsWith('/flow/query'));
    const start = performance.now();
    await page.getByRole('button', { name: 'Mở trace' }).click();
    const response = await flowResponse;
    assert.equal(response.status(), 200);
    const flow = await response.json();
    assert.equal(flow.data.mode, 'observed');
    assert.equal(flow.data.root_ref.symbol_id, fixture.symbol);
    assert.equal(flow.scope.vector_digest, fixture.vectorDigest);
    assert.equal(flow.coverage.status, 'partial');
    assert.equal(flow.data.steps.length, 2);
    assert.equal(flow.data.edges.length, 1);
    assert.ok(flow.data.steps.every(step => step.possible_or_observed === 'observed' &&
      step.order_semantics === 'causal_trace' && step.source_refs[0].content_hash === fixture.sourceHash));
    assert.deepEqual(flow.data.edges[0].evidence_refs.map(ref => ref.artifact_id), [fixture.artifactID, fixture.childArtifactID]);
    await pane.getByText('Span cha · bước 1', { exact: true }).waitFor();
    displayMS.push(Math.round(performance.now() - start));
  }
  const sourceMS = [];
  for (let i = 0; i < 12; i++) {
    const step = pane.getByRole('button', { name: i % 2 === 0 ? /^HTTP request/ : /^Handler/ });
    await step.focus();
    const sourceRead = page.waitForResponse(response => response.url().endsWith('/source/read'));
    const start = performance.now();
    await step.press('Enter');
    const response = await sourceRead;
    assert.equal(response.status(), 200);
    const body = await response.json();
    assert.equal(body.data.source_ref.path, fixture.sourcePath);
    assert.equal(body.data.source_ref.content_hash, fixture.sourceHash);
    assert.equal(body.data.content, i % 2 === 0 ? fixture.symbol : 'captureObservedArrival');
    const source = page.locator('#source-pane').getByLabel('Mã nguồn của snapshot');
    await source.getByText(body.data.content, { exact: true }).waitFor();
    sourceMS.push(Math.round(performance.now() - start));
  }

  const deniedResponse = page.waitForResponse(response => response.url().endsWith('/flow/query'));
  await id.fill('1'.repeat(64));
  await page.getByRole('button', { name: 'Mở trace' }).click();
  assert.equal((await deniedResponse).status(), 403);
  await pane.getByRole('alert').waitFor();
  assert.equal(await pane.getByText('Span cha · bước 1', { exact: true }).count(), 0);
  assert.deepEqual(outside, []);
  assert.equal(paths.filter(path => path.includes('/preview/')).length, 0);
  console.log(JSON.stringify({ check: 'T104_PACKAGED_EXPLORER_OBSERVED', status: 'PASS', browser: browser.version(),
    observedSteps: 2, exactSourceHash: true, artifactRefusal: true, disallowedRequests: outside.length,
    unauthorizedTargetActions: 0, graph_ms: Math.round(graphMS), display_ms: displayMS, source_ms: sourceMS }));
} finally {
  await browser.close();
}
